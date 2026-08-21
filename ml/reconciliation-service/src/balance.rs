//! Steady-state data reconciliation of a milk mass-balance network.
//!
//! Milk is conserved: at every interior node the inflow equals the outflow.
//! Measurement error means the raw numbers never quite do, so this module finds
//! the smallest uncertainty-weighted set of adjustments that closes every node
//! balance, and reports which streams had to move further than their own
//! instrument can plausibly explain.

use std::cmp::Ordering;
use std::collections::{HashMap, HashSet};

use mlcore::stats::compensated_sum;
use mlcore::{MlError, MlResult};

use crate::contract::{FlowMeasurement, ReconciledFlow};

pub const MAX_FLOWS: usize = 5_000;

/// The constrained system is dense and solved by elimination, so the node count
/// is what bounds the work, not the flow count.
pub const MAX_NODES: usize = 2_000;

pub const DEFAULT_GROSS_ERROR_THRESHOLD: f64 = 3.0;

/// A flow naming no node on one side crosses the system boundary.
const BOUNDARY: &str = "";

/// Unmeasured flows have no uncertainty of their own. Folding them into the
/// measured formulation with a variance far larger than any real one makes the
/// solver hand them essentially the whole adjustment a node needs, which is
/// precisely the role of an unmeasured variable: it is solved for rather than
/// corrected. The ratio is bounded rather than infinite so the constrained
/// system stays conditioned well enough for a double-precision solve.
const UNMEASURED_VARIANCE_RATIO: f64 = 1e9;

/// Rejects a pivot that elimination has reduced to noise relative to its own
/// row, which means the constraint rows are linearly dependent.
const PIVOT_TOL: f64 = 1e-12;

#[derive(Debug)]
pub struct Reconciliation {
    pub flows: Vec<ReconciledFlow>,
    pub converged: bool,
    pub residual_before: f64,
    pub residual_after: f64,
    pub suspect_flow_ids: Vec<String>,
}

pub fn reconcile(flows: &[FlowMeasurement], gross_error_threshold: f64) -> MlResult<Reconciliation> {
    validate(flows)?;

    let threshold = if gross_error_threshold.is_finite() && gross_error_threshold > 0.0 {
        gross_error_threshold
    } else {
        DEFAULT_GROSS_ERROR_THRESHOLD
    };

    let index = interior_nodes(flows);
    if index.is_empty() {
        return Err(MlError::insufficient(
            "every flow crosses the system boundary; there is no interior node to balance",
        ));
    }
    if index.len() > MAX_NODES {
        return Err(MlError::invalid(format!(
            "network has {} interior nodes, limit is {MAX_NODES}",
            index.len()
        )));
    }

    let (incidence, per_flow) = build_incidence(flows, &index);
    let variances = variances(flows);

    // An unmeasured flow contributes nothing to the imbalance being corrected;
    // its value is an output of the solve, not an input.
    let baseline: Vec<f64> = flows
        .iter()
        .map(|f| if f.unmeasured { 0.0 } else { f.measured })
        .collect();

    let imbalance = node_imbalances(&incidence, &baseline);
    let residual_before = total_imbalance(&imbalance);

    // An under-determined network is a real operational state — a leg with no
    // instrument anywhere around it — and not a caller error, so the measured
    // values come back untouched and the caller degrades on `converged`.
    let (values, converged) = match solve(&per_flow, &variances, &baseline, &imbalance, index.len())
    {
        Some(x) => (x, true),
        None => (baseline.clone(), false),
    };

    let residual_after = total_imbalance(&node_imbalances(&incidence, &values));

    let reconciled: Vec<ReconciledFlow> = flows
        .iter()
        .zip(baseline.iter())
        .zip(values.iter())
        .map(|((flow, &measured), &value)| {
            let adjustment = value - measured;
            // The measurement test: how many of its own standard uncertainties
            // a stream had to move to close the network.
            let test_statistic = if flow.unmeasured {
                0.0
            } else {
                (adjustment / flow.standard_uncertainty).abs()
            };
            ReconciledFlow {
                flow_id: flow.flow_id.clone(),
                measured,
                reconciled: value,
                adjustment,
                test_statistic,
                gross_error: !flow.unmeasured && converged && test_statistic > threshold,
                unmeasured: flow.unmeasured,
            }
        })
        .collect();

    let mut suspects: Vec<&ReconciledFlow> = reconciled.iter().filter(|f| f.gross_error).collect();
    suspects.sort_by(|a, b| {
        b.test_statistic
            .partial_cmp(&a.test_statistic)
            .unwrap_or(Ordering::Equal)
    });
    let suspect_flow_ids = suspects.iter().map(|f| f.flow_id.clone()).collect();

    Ok(Reconciliation {
        flows: reconciled,
        converged,
        residual_before,
        residual_after,
        suspect_flow_ids,
    })
}

fn validate(flows: &[FlowMeasurement]) -> MlResult<()> {
    if flows.is_empty() {
        return Err(MlError::invalid("flows must carry at least one measurement"));
    }
    if flows.len() > MAX_FLOWS {
        return Err(MlError::invalid(format!(
            "network carries {} flows, limit is {MAX_FLOWS}",
            flows.len()
        )));
    }

    let mut seen: HashSet<&str> = HashSet::with_capacity(flows.len());
    for flow in flows {
        if !seen.insert(flow.flow_id.as_str()) {
            return Err(MlError::invalid(format!(
                "flow {} appears more than once",
                flow.flow_id
            )));
        }
        if !flow.measured.is_finite() {
            return Err(MlError::invalid(format!(
                "flow {} carries a non-finite measured value",
                flow.flow_id
            )));
        }
        if flow.from_node == BOUNDARY && flow.to_node == BOUNDARY {
            return Err(MlError::invalid(format!(
                "flow {} runs from the system boundary to the system boundary",
                flow.flow_id
            )));
        }
        // A zero or negative standard uncertainty would weight the stream
        // infinitely and let one instrument dictate the whole solution.
        if !flow.unmeasured
            && (!flow.standard_uncertainty.is_finite() || flow.standard_uncertainty <= 0.0)
        {
            return Err(MlError::invalid(format!(
                "measured flow {} needs a positive, finite standard_uncertainty",
                flow.flow_id
            )));
        }
    }
    Ok(())
}

fn interior_nodes(flows: &[FlowMeasurement]) -> HashMap<&str, usize> {
    let mut index = HashMap::new();
    for flow in flows {
        for node in [flow.from_node.as_str(), flow.to_node.as_str()] {
            if node != BOUNDARY {
                let next = index.len();
                index.entry(node).or_insert(next);
            }
        }
    }
    index
}

/// Builds the node-incidence matrix `A`: rows are interior nodes, columns are
/// flows, `+1` where a flow enters the node and `-1` where it leaves.
///
/// The per-flow view returned alongside it lists the at most two nodes a flow
/// touches, which is what keeps the normal-equation assembly linear in the flow
/// count instead of quadratic in the node count.
type Incidence = (Vec<Vec<f64>>, Vec<Vec<(usize, f64)>>);

fn build_incidence(flows: &[FlowMeasurement], index: &HashMap<&str, usize>) -> Incidence {
    let mut matrix = vec![vec![0.0; flows.len()]; index.len()];
    let mut per_flow: Vec<Vec<(usize, f64)>> = Vec::with_capacity(flows.len());

    for (j, flow) in flows.iter().enumerate() {
        let to = index.get(flow.to_node.as_str()).copied();
        let from = index.get(flow.from_node.as_str()).copied();
        if let Some(i) = to {
            matrix[i][j] += 1.0;
        }
        if let Some(i) = from {
            matrix[i][j] -= 1.0;
        }

        let mut entries: Vec<(usize, f64)> = Vec::new();
        for i in to.into_iter().chain(from) {
            if entries.iter().any(|&(seen, _)| seen == i) {
                continue;
            }
            let coefficient = matrix[i][j];
            if coefficient != 0.0 {
                entries.push((i, coefficient));
            }
        }
        per_flow.push(entries);
    }

    (matrix, per_flow)
}

fn variances(flows: &[FlowMeasurement]) -> Vec<f64> {
    let widest = flows
        .iter()
        .filter(|f| !f.unmeasured)
        .map(|f| f.standard_uncertainty * f.standard_uncertainty)
        .fold(0.0f64, f64::max);
    let magnitude = flows.iter().map(|f| f.measured.abs()).fold(0.0f64, f64::max);
    let reference = widest.max(magnitude * magnitude).max(1.0);
    let unmeasured = reference * UNMEASURED_VARIANCE_RATIO;

    flows
        .iter()
        .map(|f| {
            if f.unmeasured {
                unmeasured
            } else {
                f.standard_uncertainty * f.standard_uncertainty
            }
        })
        .collect()
}

fn node_imbalances(incidence: &[Vec<f64>], x: &[f64]) -> Vec<f64> {
    incidence
        .iter()
        .map(|row| {
            let terms: Vec<f64> = row
                .iter()
                .zip(x)
                .filter(|(a, _)| **a != 0.0)
                .map(|(a, v)| a * v)
                .collect();
            compensated_sum(&terms)
        })
        .collect()
}

fn total_imbalance(imbalances: &[f64]) -> f64 {
    let magnitudes: Vec<f64> = imbalances.iter().map(|v| v.abs()).collect();
    compensated_sum(&magnitudes)
}

/// Weighted least squares under the equality constraints `A x = 0`.
///
/// Minimising `(x - x_m)^T W (x - x_m)` with `W = diag(1/sigma^2)` gives the
/// closed form `x = x_m - V A^T (A V A^T)^{-1} A x_m` with `V = W^{-1}`.
/// Returns `None` when `A V A^T` is singular.
fn solve(
    per_flow: &[Vec<(usize, f64)>],
    variances: &[f64],
    baseline: &[f64],
    imbalance: &[f64],
    node_count: usize,
) -> Option<Vec<f64>> {
    let mut system = vec![vec![0.0; node_count]; node_count];
    for (entries, &v) in per_flow.iter().zip(variances) {
        for &(i, si) in entries {
            for &(k, sk) in entries {
                system[i][k] += si * v * sk;
            }
        }
    }

    let mut rhs = imbalance.to_vec();
    let lambda = gaussian_solve(&mut system, &mut rhs)?;

    let mut x = baseline.to_vec();
    for (j, entries) in per_flow.iter().enumerate() {
        let projected: f64 = entries.iter().map(|&(i, sign)| sign * lambda[i]).sum();
        x[j] -= variances[j] * projected;
    }

    x.iter().all(|v| v.is_finite()).then_some(x)
}

/// Gaussian elimination with scaled partial pivoting.
///
/// Pivots are compared against their own row's magnitude so that a network
/// mixing precise instruments with unmeasured legs — variances spanning many
/// orders of magnitude — is not mistaken for a singular one.
fn gaussian_solve(m: &mut [Vec<f64>], rhs: &mut [f64]) -> Option<Vec<f64>> {
    let n = rhs.len();
    let mut scale: Vec<f64> = m
        .iter()
        .map(|row| row.iter().fold(0.0f64, |acc, v| acc.max(v.abs())))
        .collect();
    if scale.iter().any(|s| *s <= 0.0) {
        return None;
    }

    for k in 0..n {
        let (best, ratio) = m
            .iter()
            .zip(scale.iter())
            .enumerate()
            .skip(k)
            .map(|(i, (row, s))| (i, row[k].abs() / s))
            .fold((k, 0.0f64), |acc, cur| if cur.1 > acc.1 { cur } else { acc });
        if ratio < PIVOT_TOL {
            return None;
        }
        m.swap(k, best);
        rhs.swap(k, best);
        scale.swap(k, best);

        let (upper, lower) = m.split_at_mut(k + 1);
        let pivot_row = &upper[k];
        let pivot = pivot_row[k];
        let (rhs_upper, rhs_lower) = rhs.split_at_mut(k + 1);
        let pivot_rhs = rhs_upper[k];

        for (row, r) in lower.iter_mut().zip(rhs_lower.iter_mut()) {
            let factor = row[k] / pivot;
            if factor == 0.0 {
                continue;
            }
            for (cell, p) in row.iter_mut().zip(pivot_row.iter()).skip(k) {
                *cell -= factor * p;
            }
            *r -= factor * pivot_rhs;
        }
    }

    let mut x = vec![0.0f64; n];
    for i in (0..n).rev() {
        let row = &m[i];
        let mut acc = rhs[i];
        for (j, v) in row.iter().enumerate().skip(i + 1) {
            acc -= v * x[j];
        }
        x[i] = acc / row[i];
    }
    Some(x)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn flow(id: &str, from: &str, to: &str, measured: f64, sigma: f64) -> FlowMeasurement {
        FlowMeasurement {
            flow_id: id.to_string(),
            from_node: from.to_string(),
            to_node: to.to_string(),
            measured,
            standard_uncertainty: sigma,
            unmeasured: false,
        }
    }

    fn unmeasured(id: &str, from: &str, to: &str) -> FlowMeasurement {
        FlowMeasurement {
            flow_id: id.to_string(),
            from_node: from.to_string(),
            to_node: to.to_string(),
            measured: 0.0,
            standard_uncertainty: 0.0,
            unmeasured: true,
        }
    }

    fn by_id<'a>(r: &'a Reconciliation, id: &str) -> &'a ReconciledFlow {
        r.flows.iter().find(|f| f.flow_id == id).unwrap()
    }

    /// One inflow and one outflow at a chilling centre, both equally trusted.
    fn series_node(sigma_in: f64, sigma_out: f64) -> Vec<FlowMeasurement> {
        vec![
            flow("in", "", "cc-1", 100.0, sigma_in),
            flow("out", "cc-1", "", 98.0, sigma_out),
        ]
    }

    #[test]
    fn equal_uncertainties_split_the_gap_evenly() {
        let r = reconcile(&series_node(1.0, 1.0), 0.0).unwrap();
        assert!(r.converged);
        assert!((by_id(&r, "in").reconciled - 99.0).abs() < 1e-9);
        assert!((by_id(&r, "out").reconciled - 99.0).abs() < 1e-9);
        assert!((by_id(&r, "in").adjustment + 1.0).abs() < 1e-9);
        assert!((by_id(&r, "out").adjustment - 1.0).abs() < 1e-9);
    }

    #[test]
    fn the_larger_uncertainty_absorbs_the_larger_adjustment() {
        let r = reconcile(&series_node(1.0, 3.0), 0.0).unwrap();
        let precise = by_id(&r, "in").adjustment.abs();
        let loose = by_id(&r, "out").adjustment.abs();
        assert!(
            loose > precise,
            "the less certain stream must move further: {loose} vs {precise}"
        );
        // Variances 1 and 9 share a 2.0 gap in the ratio 1:9.
        assert!((precise - 0.2).abs() < 1e-9, "got {precise}");
        assert!((loose - 1.8).abs() < 1e-9, "got {loose}");
        assert!(r.residual_after < 1e-9);
    }

    #[test]
    fn a_splitting_node_closes_the_balance() {
        let flows = vec![
            flow("intake", "", "cc-1", 100.0, 1.0),
            flow("tanker-a", "cc-1", "", 40.0, 1.0),
            flow("tanker-b", "cc-1", "", 55.0, 1.0),
        ];
        let r = reconcile(&flows, 0.0).unwrap();
        assert!(r.converged);
        assert!(r.residual_after < 1e-9, "residual {}", r.residual_after);
        let closed = by_id(&r, "intake").reconciled
            - by_id(&r, "tanker-a").reconciled
            - by_id(&r, "tanker-b").reconciled;
        assert!(closed.abs() < 1e-9, "node did not close: {closed}");
    }

    #[test]
    fn a_balanced_network_is_left_alone() {
        let flows = vec![
            flow("in", "", "cc-1", 100.0, 1.0),
            flow("out", "cc-1", "", 100.0, 1.0),
        ];
        let r = reconcile(&flows, 0.0).unwrap();
        assert!(r.converged);
        assert!(r.residual_before < 1e-12);
        assert!(r.residual_after < 1e-12);
        assert!(r.flows.iter().all(|f| f.adjustment.abs() < 1e-12));
        assert!(r.suspect_flow_ids.is_empty());
    }

    /// A three-node chain with the middle leg over-reading by 30.
    fn chain_with_a_bad_middle() -> Vec<FlowMeasurement> {
        vec![
            flow("collect", "", "cc-1", 100.0, 1.0),
            flow("haul", "cc-1", "plant-1", 130.0, 1.0),
            flow("intake", "plant-1", "", 100.0, 1.0),
        ]
    }

    #[test]
    fn the_worst_flow_leads_the_suspect_list() {
        let r = reconcile(&chain_with_a_bad_middle(), 0.0).unwrap();
        assert!(r.converged);
        assert_eq!(
            r.suspect_flow_ids.first().map(String::as_str),
            Some("haul"),
            "suspects were {:?}",
            r.suspect_flow_ids
        );
        let haul = by_id(&r, "haul").test_statistic;
        assert!(haul > by_id(&r, "collect").test_statistic);
        assert!(haul > by_id(&r, "intake").test_statistic);

        let stats: Vec<f64> = r
            .suspect_flow_ids
            .iter()
            .map(|id| by_id(&r, id).test_statistic)
            .collect();
        assert!(
            stats.windows(2).all(|w| w[0] >= w[1]),
            "suspects are not worst-first: {stats:?}"
        );
    }

    #[test]
    fn a_generous_threshold_nominates_nobody() {
        let r = reconcile(&chain_with_a_bad_middle(), 1_000.0).unwrap();
        assert!(r.converged);
        assert!(r.suspect_flow_ids.is_empty());
        assert!(r.flows.iter().all(|f| !f.gross_error));
    }

    #[test]
    fn reconciliation_reduces_the_total_imbalance() {
        let r = reconcile(&chain_with_a_bad_middle(), 0.0).unwrap();
        assert!(r.converged);
        assert!(r.residual_before > 1.0, "test network is already balanced");
        assert!(
            r.residual_after < r.residual_before,
            "{} did not improve on {}",
            r.residual_after,
            r.residual_before
        );
        assert!(r.residual_after < 1e-9);
    }

    #[test]
    fn an_under_determined_network_does_not_converge() {
        // A closed loop with no boundary flow: the two node balances are the
        // same equation twice over, so the constrained system is singular.
        let flows = vec![
            flow("out", "cc-1", "plant-1", 100.0, 1.0),
            flow("back", "plant-1", "cc-1", 98.0, 1.0),
        ];
        let r = reconcile(&flows, 0.0).unwrap();
        assert!(!r.converged);
        assert!((by_id(&r, "out").reconciled - 100.0).abs() < 1e-12);
        assert!((by_id(&r, "back").reconciled - 98.0).abs() < 1e-12);
        assert!(r.flows.iter().all(|f| f.adjustment.abs() < 1e-12));
        assert!(r.flows.iter().all(|f| !f.gross_error));
        assert!(r.suspect_flow_ids.is_empty());
        assert!(r.residual_before > 0.0);
        assert!((r.residual_after - r.residual_before).abs() < 1e-12);
    }

    #[test]
    fn an_unmeasured_flow_is_solved_for() {
        let flows = vec![
            flow("intake", "", "cc-1", 100.0, 1.0),
            unmeasured("dispatch", "cc-1", ""),
        ];
        let r = reconcile(&flows, 0.0).unwrap();
        assert!(r.converged);

        let dispatch = by_id(&r, "dispatch");
        assert!(
            (dispatch.reconciled - 100.0).abs() < 1e-4,
            "unmeasured leg solved as {}",
            dispatch.reconciled
        );
        assert!(!dispatch.gross_error, "an unmeasured flow has nothing to fail");
        assert_eq!(dispatch.test_statistic, 0.0);
        assert!(dispatch.unmeasured);

        // The measured leg is trusted: the unmeasured one takes the strain.
        assert!(by_id(&r, "intake").adjustment.abs() < 1e-4);
        assert!(r.residual_after < 1e-6, "residual {}", r.residual_after);
        assert!(r.suspect_flow_ids.is_empty());
    }

    #[test]
    fn an_unmeasured_flow_is_never_a_gross_error() {
        let flows = vec![
            flow("intake", "", "cc-1", 100.0, 1.0),
            flow("dispatch", "cc-1", "", 60.0, 1.0),
            unmeasured("loss", "cc-1", ""),
        ];
        let r = reconcile(&flows, 0.0).unwrap();
        assert!(r.converged);
        assert!(!by_id(&r, "loss").gross_error);
        assert!(!r.suspect_flow_ids.iter().any(|id| id == "loss"));
        // The unmeasured leg absorbs the 40 unit shortfall.
        assert!(
            (by_id(&r, "loss").reconciled - 40.0).abs() < 1e-3,
            "loss solved as {}",
            by_id(&r, "loss").reconciled
        );
    }

    #[test]
    fn an_empty_flow_list_is_rejected() {
        let err = reconcile(&[], 0.0).unwrap_err();
        assert!(matches!(err, MlError::InvalidArgument(_)), "{err:?}");
    }

    #[test]
    fn a_duplicate_flow_id_is_rejected() {
        let flows = vec![
            flow("in", "", "cc-1", 100.0, 1.0),
            flow("in", "cc-1", "", 98.0, 1.0),
        ];
        let err = reconcile(&flows, 0.0).unwrap_err();
        assert!(matches!(err, MlError::InvalidArgument(_)), "{err:?}");
    }

    #[test]
    fn a_zero_standard_uncertainty_is_rejected() {
        let err = reconcile(&series_node(1.0, 0.0), 0.0).unwrap_err();
        assert!(matches!(err, MlError::InvalidArgument(_)), "{err:?}");
        let err = reconcile(&series_node(-1.0, 1.0), 0.0).unwrap_err();
        assert!(matches!(err, MlError::InvalidArgument(_)), "{err:?}");
    }

    #[test]
    fn a_boundary_to_boundary_flow_is_rejected() {
        let flows = vec![flow("nowhere", "", "", 100.0, 1.0)];
        let err = reconcile(&flows, 0.0).unwrap_err();
        assert!(matches!(err, MlError::InvalidArgument(_)), "{err:?}");
    }

    #[test]
    fn a_non_finite_measurement_is_rejected() {
        let flows = vec![flow("in", "", "cc-1", f64::NAN, 1.0)];
        let err = reconcile(&flows, 0.0).unwrap_err();
        assert!(matches!(err, MlError::InvalidArgument(_)), "{err:?}");
    }

    #[test]
    fn too_many_flows_are_rejected() {
        let flows: Vec<FlowMeasurement> = (0..MAX_FLOWS + 1)
            .map(|i| flow(&format!("f{i}"), "", "cc-1", 1.0, 1.0))
            .collect();
        let err = reconcile(&flows, 0.0).unwrap_err();
        assert!(matches!(err, MlError::InvalidArgument(_)), "{err:?}");
    }

    #[test]
    fn too_many_nodes_are_rejected() {
        let flows: Vec<FlowMeasurement> = (0..MAX_NODES + 1)
            .map(|i| flow(&format!("f{i}"), "", &format!("cc-{i}"), 1.0, 1.0))
            .collect();
        let err = reconcile(&flows, 0.0).unwrap_err();
        assert!(matches!(err, MlError::InvalidArgument(_)), "{err:?}");
    }

    #[test]
    fn a_wider_network_still_closes_every_node() {
        let flows = vec![
            flow("farm-a", "", "cc-1", 310.0, 4.0),
            flow("farm-b", "", "cc-1", 275.0, 4.0),
            flow("cc-out", "cc-1", "tanker-7", 570.0, 6.0),
            flow("tanker-in", "tanker-7", "plant-1", 566.0, 6.0),
            flow("plant-intake", "plant-1", "", 572.0, 5.0),
        ];
        let r = reconcile(&flows, 0.0).unwrap();
        assert!(r.converged);
        assert!(r.residual_before > 1.0);
        assert!(r.residual_after < 1e-9, "residual {}", r.residual_after);

        let cc = by_id(&r, "farm-a").reconciled + by_id(&r, "farm-b").reconciled
            - by_id(&r, "cc-out").reconciled;
        assert!(cc.abs() < 1e-9, "chilling centre did not close: {cc}");
        let tanker = by_id(&r, "cc-out").reconciled - by_id(&r, "tanker-in").reconciled;
        assert!(tanker.abs() < 1e-9, "tanker did not close: {tanker}");
        let plant = by_id(&r, "tanker-in").reconciled - by_id(&r, "plant-intake").reconciled;
        assert!(plant.abs() < 1e-9, "plant did not close: {plant}");
    }
}
