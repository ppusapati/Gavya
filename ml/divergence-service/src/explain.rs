//! Hypothesis generation for settlement divergences the deterministic
//! classifier could not attribute.
//!
//! The platform runs in shadow mode: an incumbent system's settlement is
//! imported and independently recomputed, and a money difference between the
//! two is a divergence. A deterministic classifier in Go has already claimed
//! every divergence whose evidence is conclusive; only its `UNEXPLAINED`
//! leftovers arrive here. Nothing this module returns is authoritative — it is
//! a hypothesis filed beside the deterministic verdict for a human to check.
//!
//! Two kinds of evidence are weighed. Direct-attribution rules read the
//! deterministic feature deltas the Go classifier already computed, and a
//! nearest-neighbour vote reads divergences a human has already adjudicated.
//! Both are bounded well short of certainty, and when neither speaks clearly
//! the service abstains: a wrong hypothesis costs an auditor a day chasing the
//! wrong mechanism, whereas an abstention costs only the review that was
//! already going to happen.

use std::cmp::Ordering;
use std::collections::{BTreeMap, BTreeSet};

use mlcore::{MlError, MlResult};

use crate::contract::{DivergenceHypothesis, ExplainDivergenceRequest};

/// Confidence floor below which the service reports nothing at all.
pub const ABSTAIN_BELOW: f64 = 0.55;

/// No hypothesis is ever emitted above this: the service does not claim
/// certainty about a difference a deterministic classifier already failed to
/// attribute.
pub const MAX_CONFIDENCE: f64 = 0.95;

pub const MAX_HYPOTHESES: usize = 3;
pub const MAX_PRECEDENTS: usize = 500;
pub const K_NEIGHBOURS: usize = 5;

pub const CLASS_ROUNDING: &str = "ROUNDING_DIFFERENCE";
pub const CLASS_INPUT: &str = "INPUT_DIFFERENCE";
pub const CLASS_POLICY: &str = "POLICY_DIFFERENCE";
pub const CLASS_RECOVERY: &str = "RECOVERY_DIFFERENCE";

const FEATURE_ROUNDING_RESIDUAL: &str = "rounding_residual_minor_units";
const FEATURE_POLICY_VERSION_DELTA: &str = "policy_version_delta";
const FEATURE_RECOVERY_AMOUNT: &str = "recovery_amount_minor_units";

const INPUT_FEATURES: [&str; 4] = [
    "input_delta_quantity",
    "input_delta_fat",
    "input_delta_snf",
    "input_delta_rate",
];

const RECOVERY_COVERAGE_FLOOR: f64 = 0.99;
const RECOVERY_MATCH_FLOOR: f64 = 0.95;
const ROUNDING_TOLERANCE_MINOR_UNITS: f64 = 2.0;
const PRECEDENT_CEILING: f64 = 0.95;

/// Number of concurring neighbours at which precedent evidence is taken at full
/// strength. Below it the vote is scaled down, so one lucky match cannot carry
/// a classification on its own.
const MIN_PRECEDENT_SUPPORT: f64 = 3.0;

/// Weight retained by a precedent whose delta runs the opposite way to the
/// request's. External-over-shadow and shadow-over-external divergences are
/// produced by different mechanisms — an unapplied recovery and a
/// double-applied one are not the same story — so such a precedent is evidence,
/// but weaker evidence.
const OPPOSITE_SIGN_WEIGHT: f64 = 0.5;

#[derive(Debug)]
pub struct Explanation {
    pub hypotheses: Vec<DivergenceHypothesis>,
    pub abstained: bool,
}

impl Explanation {
    fn abstain() -> Self {
        Self {
            hypotheses: Vec::new(),
            abstained: true,
        }
    }
}

#[derive(Default)]
struct Candidate {
    confidence: f64,
    rationales: Vec<String>,
    fields: BTreeSet<String>,
    precedent_ids: Vec<String>,
}

struct Neighbour {
    id: String,
    classification: String,
    similarity: f64,
    weight: f64,
    fields: Vec<String>,
}

pub fn explain(req: &ExplainDivergenceRequest) -> MlResult<Explanation> {
    validate(req)?;

    let abs_delta = (req.delta_minor_units as f64).abs();

    // No feature moved, so there is nothing to attribute the difference to. The
    // Go classifier reached the same dead end; repeating it as a guess would
    // add noise, not information.
    if req.features.is_empty() || req.features.values().all(|v| *v == 0.0) {
        return Ok(Explanation::abstain());
    }

    let mut candidates: BTreeMap<String, Candidate> = BTreeMap::new();
    apply_rules(req, abs_delta, &mut candidates);
    apply_precedents(req, abs_delta, &mut candidates);

    let mut hypotheses: Vec<DivergenceHypothesis> = candidates
        .into_iter()
        .map(|(classification, c)| DivergenceHypothesis {
            classification,
            confidence: c.confidence.min(MAX_CONFIDENCE),
            rationale: c.rationales.join("; "),
            supporting_fields: c.fields.into_iter().collect(),
            precedent_ids: c.precedent_ids,
        })
        .collect();

    // Ties broken by classification so an identical request always produces an
    // identical ordering; a reviewer comparing two runs must not see churn.
    hypotheses.sort_by(|a, b| {
        b.confidence
            .partial_cmp(&a.confidence)
            .unwrap_or(Ordering::Equal)
            .then_with(|| a.classification.cmp(&b.classification))
    });
    hypotheses.truncate(MAX_HYPOTHESES);

    let top = hypotheses.first().map(|h| h.confidence).unwrap_or(0.0);
    if top < ABSTAIN_BELOW {
        return Ok(Explanation::abstain());
    }

    Ok(Explanation {
        hypotheses,
        abstained: false,
    })
}

fn validate(req: &ExplainDivergenceRequest) -> MlResult<()> {
    if req.divergence_id.trim().is_empty() {
        return Err(MlError::invalid("divergence_id is required"));
    }
    if req.delta_minor_units == 0 {
        return Err(MlError::invalid(
            "delta_minor_units is zero, which is a MATCH; the deterministic classifier must resolve it and never route it here",
        ));
    }
    if req.peer_history.len() > MAX_PRECEDENTS {
        return Err(MlError::invalid(format!(
            "peer_history carries {} precedents, limit is {MAX_PRECEDENTS}",
            req.peer_history.len()
        )));
    }
    reject_non_finite(&req.divergence_id, &req.features)?;
    for p in &req.peer_history {
        reject_non_finite(&p.divergence_id, &p.features)?;
    }
    Ok(())
}

/// NaN and infinity would survive every ratio and distance below and surface as
/// a hypothesis with a meaningless confidence, so they are refused at the
/// boundary.
fn reject_non_finite(id: &str, features: &BTreeMap<String, f64>) -> MlResult<()> {
    for (key, value) in features {
        if !value.is_finite() {
            return Err(MlError::invalid(format!(
                "divergence {id} carries a non-finite value for feature {key}"
            )));
        }
    }
    Ok(())
}

fn apply_rules(req: &ExplainDivergenceRequest, abs_delta: f64, out: &mut BTreeMap<String, Candidate>) {
    let delta = req.delta_minor_units;

    if let Some(recovery) = nonzero(req, FEATURE_RECOVERY_AMOUNT) {
        let coverage = recovery.abs() / abs_delta;
        // Symmetric match quality: a recovery ten times the delta explains it no
        // better than one a tenth of it, so both fall away from 1.0 the same way.
        let match_quality = if coverage <= 1.0 { coverage } else { 1.0 / coverage };

        if coverage >= RECOVERY_COVERAGE_FLOOR && match_quality >= RECOVERY_MATCH_FLOOR {
            merge(
                out,
                CLASS_RECOVERY,
                0.90 + 0.05 * match_quality,
                format!(
                    "recovery of {:.0} minor units accounts for {:.1}% of the {} minor unit delta",
                    recovery.abs(),
                    coverage * 100.0,
                    delta
                ),
                [FEATURE_RECOVERY_AMOUNT],
                Vec::new(),
            );
        } else if match_quality >= 0.5 {
            merge(
                out,
                CLASS_RECOVERY,
                0.35 + 0.35 * match_quality,
                format!(
                    "recovery of {:.0} minor units covers {:.1}% of the {} minor unit delta, so it accounts for part of the difference but not all of it",
                    recovery.abs(),
                    coverage * 100.0,
                    delta
                ),
                [FEATURE_RECOVERY_AMOUNT],
                Vec::new(),
            );
        }
    }

    if abs_delta <= ROUNDING_TOLERANCE_MINOR_UNITS {
        if let Some(residual) = nonzero(req, FEATURE_ROUNDING_RESIDUAL) {
            // A one or two minor unit difference is not arithmetically reachable
            // by any other mechanism: a rate, quantity or policy change moves
            // money by more than the smallest coin.
            let exact = (residual.abs() - abs_delta).abs() < f64::EPSILON;
            merge(
                out,
                CLASS_ROUNDING,
                if exact { MAX_CONFIDENCE } else { 0.90 },
                format!(
                    "the delta is {delta} minor units and a rounding residual of {:.0} minor units is present; a difference this small cannot be produced by an input, rate or policy change",
                    residual.abs()
                ),
                [FEATURE_ROUNDING_RESIDUAL],
                Vec::new(),
            );
        }
    }

    if let Some(policy_delta) = nonzero(req, FEATURE_POLICY_VERSION_DELTA) {
        // Differing policy versions are correlation, not causation: the pricing
        // policy moved between the two computations, but nothing in the feature
        // set shows that move produced this delta. Capped short of the
        // direct-attribution band for exactly that reason.
        let steps = policy_delta.abs().min(3.0);
        merge(
            out,
            CLASS_POLICY,
            0.60 + 0.05 * steps,
            format!(
                "the external and shadow computations used policy versions {:.0} apart, which is consistent with the {delta} minor unit delta but does not by itself show the version change caused it",
                policy_delta.abs()
            ),
            [FEATURE_POLICY_VERSION_DELTA],
            Vec::new(),
        );
    }

    let moved: Vec<(&str, f64)> = INPUT_FEATURES
        .iter()
        .filter_map(|k| nonzero(req, k).map(|v| (*k, v)))
        .collect();
    if !moved.is_empty() {
        let effect: f64 = moved.iter().map(|(_, v)| v.abs()).sum();
        let contribution = effect / abs_delta;
        // The input deltas are in litres, percentage points and rate units, not
        // money, so their magnitude only bounds how plausible an explanation
        // they are — it never proves one. That is why this rule saturates below
        // the direct-attribution band even for a perfect magnitude match.
        let match_quality = if contribution <= 1.0 {
            contribution
        } else {
            1.0 / contribution
        };
        let named = moved
            .iter()
            .map(|(k, v)| format!("{k}={v:.4}"))
            .collect::<Vec<_>>()
            .join(", ");
        merge(
            out,
            CLASS_INPUT,
            0.40 + 0.40 * match_quality,
            format!(
                "settlement inputs differ ({named}); their combined magnitude {effect:.4} is {contribution:.2}x the {delta} minor unit delta, which is consistent with the inputs accounting for it"
            ),
            moved.iter().map(|(k, _)| *k),
            Vec::new(),
        );
    }
}

fn apply_precedents(
    req: &ExplainDivergenceRequest,
    abs_delta: f64,
    out: &mut BTreeMap<String, Candidate>,
) {
    if req.peer_history.is_empty() {
        return;
    }

    // The absolute delta spans orders of magnitude between a smallholder and a
    // cooperative, so a precedent is only comparable once every feature is
    // expressed as a fraction of its own divergence's delta.
    let subject = ratio_vector(&req.features, abs_delta);
    let request_positive = req.delta_minor_units > 0;

    let mut neighbours: Vec<Neighbour> = Vec::new();
    for p in &req.peer_history {
        // A zero-delta precedent is a MATCH; it has no attribution to lend and
        // cannot be ratio-normalised anyway.
        if p.delta_minor_units == 0 || p.classification.trim().is_empty() {
            continue;
        }
        let peer = ratio_vector(&p.features, (p.delta_minor_units as f64).abs());
        let similarity = 1.0 / (1.0 + euclidean(&subject, &peer));
        let weight = if (p.delta_minor_units > 0) == request_positive {
            similarity
        } else {
            similarity * OPPOSITE_SIGN_WEIGHT
        };
        neighbours.push(Neighbour {
            id: p.divergence_id.clone(),
            classification: p.classification.clone(),
            similarity,
            weight,
            fields: p.features.keys().cloned().collect(),
        });
    }

    // Neighbours are chosen by raw distance; the opposite-sign discount weighs
    // the evidence a neighbour gives, it does not decide which are nearest.
    neighbours.sort_by(|a, b| {
        b.similarity
            .partial_cmp(&a.similarity)
            .unwrap_or(Ordering::Equal)
            .then_with(|| a.id.cmp(&b.id))
    });
    neighbours.truncate(K_NEIGHBOURS);

    let total: f64 = neighbours.iter().map(|n| n.weight).sum();
    if total <= 0.0 {
        return;
    }
    let considered = neighbours.len();

    let mut by_class: BTreeMap<&str, Vec<&Neighbour>> = BTreeMap::new();
    for n in &neighbours {
        by_class.entry(n.classification.as_str()).or_default().push(n);
    }

    for (classification, group) in by_class {
        // A MATCH or UNEXPLAINED precedent still dilutes the vote above, but it
        // carries no attribution that could be transferred to this divergence.
        if !is_attributable(classification) {
            continue;
        }
        let class_weight: f64 = group.iter().map(|n| n.weight).sum();
        let mean_weight = class_weight / group.len() as f64;
        let share = class_weight / total;
        let support = (group.len() as f64 / MIN_PRECEDENT_SUPPORT).min(1.0);
        let confidence = PRECEDENT_CEILING * share * mean_weight * support;

        let ids: Vec<String> = group.iter().map(|n| n.id.clone()).collect();
        let fields: BTreeSet<String> = subject
            .keys()
            .cloned()
            .chain(group.iter().flat_map(|n| n.fields.iter().cloned()))
            .collect();

        merge(
            out,
            classification,
            confidence,
            format!(
                "{} of the {considered} nearest adjudicated divergences were classified {classification}, holding {:.0}% of the neighbourhood evidence at a mean match strength of {mean_weight:.2} (precedents: {})",
                group.len(),
                share * 100.0,
                ids.join(", ")
            ),
            fields,
            ids,
        );
    }
}

/// Combines a rule-derived and a precedent-derived confidence for the same
/// classification by taking the larger, never the sum. Two weak signals
/// agreeing is still weak evidence, and adding them would let a pile of guesses
/// manufacture a certainty neither of them supports.
fn merge<F, S>(
    out: &mut BTreeMap<String, Candidate>,
    classification: &str,
    confidence: f64,
    rationale: String,
    fields: F,
    precedent_ids: Vec<String>,
) where
    F: IntoIterator<Item = S>,
    S: Into<String>,
{
    let entry = out.entry(classification.to_string()).or_default();
    entry.confidence = entry.confidence.max(confidence.min(MAX_CONFIDENCE));
    entry.rationales.push(rationale);
    entry.fields.extend(fields.into_iter().map(Into::into));
    entry.precedent_ids.extend(precedent_ids);
}

fn is_attributable(classification: &str) -> bool {
    matches!(
        classification,
        CLASS_ROUNDING | CLASS_INPUT | CLASS_POLICY | CLASS_RECOVERY
    )
}

fn nonzero(req: &ExplainDivergenceRequest, key: &str) -> Option<f64> {
    req.features.get(key).copied().filter(|v| *v != 0.0)
}

fn ratio_vector(features: &BTreeMap<String, f64>, abs_delta: f64) -> BTreeMap<String, f64> {
    features
        .iter()
        .map(|(k, v)| (k.clone(), v / abs_delta))
        .collect()
}

/// Euclidean distance over the union of both key sets, a key absent on either
/// side reading as no movement.
fn euclidean(a: &BTreeMap<String, f64>, b: &BTreeMap<String, f64>) -> f64 {
    let keys: BTreeSet<&String> = a.keys().chain(b.keys()).collect();
    keys.into_iter()
        .map(|k| {
            let d = a.get(k).copied().unwrap_or(0.0) - b.get(k).copied().unwrap_or(0.0);
            d * d
        })
        .sum::<f64>()
        .sqrt()
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::contract::DivergencePrecedent;

    fn features(pairs: &[(&str, f64)]) -> BTreeMap<String, f64> {
        pairs.iter().map(|(k, v)| (k.to_string(), *v)).collect()
    }

    fn request(delta: i64, pairs: &[(&str, f64)]) -> ExplainDivergenceRequest {
        ExplainDivergenceRequest {
            tenant_id: "tenant-1".to_string(),
            divergence_id: "div-1".to_string(),
            currency: "INR".to_string(),
            delta_minor_units: delta,
            features: features(pairs),
            labels: BTreeMap::new(),
            peer_history: Vec::new(),
        }
    }

    fn precedent(id: &str, class: &str, delta: i64, pairs: &[(&str, f64)]) -> DivergencePrecedent {
        DivergencePrecedent {
            divergence_id: id.to_string(),
            classification: class.to_string(),
            delta_minor_units: delta,
            features: features(pairs),
        }
    }

    fn top(e: &Explanation) -> &DivergenceHypothesis {
        e.hypotheses.first().expect("expected a hypothesis")
    }

    fn confidence_of(e: &Explanation, class: &str) -> f64 {
        e.hypotheses
            .iter()
            .find(|h| h.classification == class)
            .map(|h| h.confidence)
            .unwrap_or_else(|| panic!("no {class} hypothesis in {:?}", e.hypotheses))
    }

    #[test]
    fn recovery_covering_the_whole_delta_is_attributed_with_high_confidence() {
        let req = request(4510, &[("recovery_amount_minor_units", 4500.0)]);
        let out = explain(&req).unwrap();

        assert!(!out.abstained);
        let h = top(&out);
        assert_eq!(h.classification, CLASS_RECOVERY);
        assert!(h.confidence >= 0.9, "confidence was {}", h.confidence);
        assert_eq!(
            h.supporting_fields,
            vec!["recovery_amount_minor_units".to_string()]
        );
        assert!(
            h.rationale.contains("4500") && h.rationale.contains("4510"),
            "rationale must cite the numbers: {}",
            h.rationale
        );
    }

    #[test]
    fn a_single_minor_unit_with_a_residual_is_rounding() {
        let req = request(1, &[("rounding_residual_minor_units", 1.0)]);
        let out = explain(&req).unwrap();

        assert!(!out.abstained);
        let h = top(&out);
        assert_eq!(h.classification, CLASS_ROUNDING);
        assert!(h.confidence >= 0.9, "confidence was {}", h.confidence);
    }

    #[test]
    fn a_large_delta_with_only_a_rounding_residual_is_not_rounding() {
        // Two minor units of rounding cannot produce a delta of 90000, so the
        // residual must not be allowed to claim it.
        let req = request(90_000, &[("rounding_residual_minor_units", 2.0)]);
        let out = explain(&req).unwrap();

        assert!(out.abstained);
        assert!(out.hypotheses.is_empty());
    }

    #[test]
    fn an_empty_feature_map_abstains() {
        let req = request(1234, &[]);
        let out = explain(&req).unwrap();

        assert!(out.abstained);
        assert!(out.hypotheses.is_empty());
    }

    #[test]
    fn an_all_zero_feature_map_abstains() {
        let req = request(
            1234,
            &[
                ("rounding_residual_minor_units", 0.0),
                ("input_delta_quantity", 0.0),
                ("policy_version_delta", 0.0),
                ("recovery_amount_minor_units", 0.0),
            ],
        );
        let out = explain(&req).unwrap();

        assert!(out.abstained);
        assert!(out.hypotheses.is_empty());
    }

    #[test]
    fn a_weak_ambiguous_signal_abstains_rather_than_guessing() {
        // A 0.05 litre input wobble against a 100000 minor unit delta explains
        // essentially none of it; guessing INPUT_DIFFERENCE here would send an
        // auditor to the wrong ledger.
        let req = request(
            100_000,
            &[
                ("input_delta_quantity", 0.05),
                ("days_between_computations", 2.0),
            ],
        );
        let out = explain(&req).unwrap();

        assert!(out.abstained, "expected abstention, got {:?}", out.hypotheses);
        assert!(out.hypotheses.is_empty());
    }

    #[test]
    fn concurring_close_precedents_raise_their_classification_to_the_top() {
        let base = &[("policy_version_delta", 1.0), ("input_delta_quantity", 100.0)];

        let without = explain(&request(1000, base)).unwrap();
        assert_eq!(
            top(&without).classification,
            CLASS_POLICY,
            "rules alone should favour the policy version change"
        );

        let mut req = request(1000, base);
        req.peer_history = (0..5)
            .map(|i| precedent(&format!("p{i}"), CLASS_INPUT, 1000, base))
            .collect();
        let with = explain(&req).unwrap();

        assert!(!with.abstained);
        let h = top(&with);
        assert_eq!(h.classification, CLASS_INPUT);
        assert!(
            h.confidence > confidence_of(&without, CLASS_INPUT),
            "precedents must strengthen the classification they support"
        );
        assert_eq!(h.precedent_ids.len(), 5);
        assert!(
            h.rationale.contains("p0"),
            "rationale must name the precedents: {}",
            h.rationale
        );
    }

    #[test]
    fn confidence_never_exceeds_the_cap_even_with_overwhelming_evidence() {
        let base = &[("recovery_amount_minor_units", 2000.0)];
        let mut req = request(2000, base);
        req.peer_history = (0..5)
            .map(|i| precedent(&format!("p{i}"), CLASS_RECOVERY, 2000, base))
            .collect();
        let out = explain(&req).unwrap();

        assert_eq!(top(&out).classification, CLASS_RECOVERY);
        for h in &out.hypotheses {
            assert!(
                h.confidence <= MAX_CONFIDENCE,
                "{} reported {}",
                h.classification,
                h.confidence
            );
        }
    }

    #[test]
    fn at_most_three_hypotheses_are_returned() {
        let req = request(
            2,
            &[
                ("recovery_amount_minor_units", 2.0),
                ("rounding_residual_minor_units", 1.0),
                ("policy_version_delta", 2.0),
                ("input_delta_fat", 2.0),
            ],
        );
        let out = explain(&req).unwrap();

        assert_eq!(
            out.hypotheses.len(),
            MAX_HYPOTHESES,
            "four classifications scored, so exactly three should survive"
        );
        assert!(
            out.hypotheses.iter().all(|h| h.classification != CLASS_POLICY),
            "the weakest classification should have been dropped"
        );
    }

    #[test]
    fn hypotheses_are_sorted_by_descending_confidence() {
        let req = request(
            2,
            &[
                ("recovery_amount_minor_units", 2.0),
                ("rounding_residual_minor_units", 1.0),
                ("policy_version_delta", 2.0),
                ("input_delta_fat", 2.0),
            ],
        );
        let out = explain(&req).unwrap();

        assert!(out.hypotheses.len() > 1);
        for pair in out.hypotheses.windows(2) {
            assert!(
                pair[0].confidence >= pair[1].confidence,
                "{:?} is not sorted",
                out.hypotheses
            );
        }
    }

    #[test]
    fn an_opposite_sign_precedent_counts_for_less() {
        let base = &[("input_delta_quantity", 900.0)];

        let mut same = request(1000, base);
        same.peer_history = (0..3)
            .map(|i| precedent(&format!("s{i}"), CLASS_INPUT, 1000, base))
            .collect();

        let mut opposite = request(1000, base);
        opposite.peer_history = (0..3)
            .map(|i| precedent(&format!("o{i}"), CLASS_INPUT, -1000, base))
            .collect();

        let same_out = explain(&same).unwrap();
        let opposite_out = explain(&opposite).unwrap();

        assert!(!same_out.abstained && !opposite_out.abstained);
        assert!(
            confidence_of(&same_out, CLASS_INPUT) > confidence_of(&opposite_out, CLASS_INPUT),
            "an opposite-sign precedent must not carry the same weight as a same-sign one"
        );
    }

    #[test]
    fn a_zero_delta_is_rejected_because_it_is_a_match() {
        let err = explain(&request(0, &[("rounding_residual_minor_units", 1.0)])).unwrap_err();
        assert!(matches!(err, MlError::InvalidArgument(_)), "got {err:?}");
    }

    #[test]
    fn a_missing_divergence_id_is_rejected() {
        let mut req = request(100, &[("policy_version_delta", 1.0)]);
        req.divergence_id = "   ".to_string();
        assert!(matches!(
            explain(&req).unwrap_err(),
            MlError::InvalidArgument(_)
        ));
    }

    #[test]
    fn a_non_finite_feature_is_rejected() {
        let req = request(100, &[("input_delta_fat", f64::NAN)]);
        assert!(matches!(
            explain(&req).unwrap_err(),
            MlError::InvalidArgument(_)
        ));
    }

    #[test]
    fn too_many_precedents_are_rejected() {
        let mut req = request(1000, &[("input_delta_quantity", 900.0)]);
        req.peer_history = (0..=MAX_PRECEDENTS)
            .map(|i| {
                precedent(
                    &format!("p{i}"),
                    CLASS_INPUT,
                    1000,
                    &[("input_delta_quantity", 900.0)],
                )
            })
            .collect();
        assert!(matches!(
            explain(&req).unwrap_err(),
            MlError::InvalidArgument(_)
        ));
    }

    #[test]
    fn unattributable_precedents_dilute_rather_than_explain() {
        let base = &[("input_delta_quantity", 900.0)];
        let mut req = request(1000, base);
        req.peer_history = vec![
            precedent("a", CLASS_INPUT, 1000, base),
            precedent("b", "UNEXPLAINED", 1000, base),
            precedent("c", "MATCH", 1000, base),
        ];
        let out = explain(&req).unwrap();

        assert!(
            out.hypotheses
                .iter()
                .all(|h| is_attributable(&h.classification)),
            "an UNEXPLAINED precedent must never become a hypothesis"
        );
    }
}
