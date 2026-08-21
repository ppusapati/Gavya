//! Evaluation of measurement uncertainty following the GUM (ISO/IEC Guide 98-3).
//!
//! An uncertainty budget is assembled from named components, each of which is a
//! half-width limit converted to a standard uncertainty by the divisor its
//! distribution implies, scaled by a sensitivity coefficient. The components
//! combine in quadrature; the combined standard uncertainty is expanded by a
//! coverage factor drawn from the effective degrees of freedom.
//!
//! Every component is reported back, because a producer's payment can turn on
//! the width of this interval and a reviewer must be able to recompute it.

use std::collections::BTreeMap;

use mlcore::stats::{compensated_sum, coverage_factor, std_error_of_mean, welch_satterthwaite};
use mlcore::{MlError, MlResult};

use crate::contract::{EstimateUncertaintyRequest, SeriesPoint, UncertaintyComponent};

pub const TYPE_A: &str = "A";
pub const TYPE_B: &str = "B";

pub const DIST_NORMAL: &str = "normal";
pub const DIST_RECTANGULAR: &str = "rectangular";
pub const DIST_TRIANGULAR: &str = "triangular";

pub const DEFAULT_COVERAGE_PROBABILITY: f64 = 0.95;

/// A standard deviation from two or three readings is not evidence; the Type A
/// evaluation is refused below this many replicates.
pub const MIN_REPLICATES: usize = 6;

/// Below this the fit is returned but marked unreliable, so callers keep the
/// Type B budget rather than adopting the fitted term outright.
pub const SUFFICIENT_REPLICATES: usize = 10;

pub const MODEL_VOLUME_FLOWMETER: &str = "milk.volume.flowmeter.v1";
pub const MODEL_FAT_GERBER: &str = "milk.fat.gerber.v1";
pub const MODEL_SNF_LACTOMETER: &str = "milk.snf.lactometer.v1";
pub const MODEL_MASS_PLATFORM_SCALE: &str = "milk.mass.platform_scale.v1";

const MODEL_IDS: [&str; 4] = [
    MODEL_VOLUME_FLOWMETER,
    MODEL_FAT_GERBER,
    MODEL_SNF_LACTOMETER,
    MODEL_MASS_PLATFORM_SCALE,
];

/// Cubical expansion coefficient of whole milk, per degree Celsius. A flowmeter
/// registers volume at line temperature, not at the reference temperature the
/// settlement is expressed in.
const MILK_VOLUMETRIC_EXPANSION_PER_C: f64 = 0.000_21;

/// A calibration certificate quotes an expanded uncertainty at k = 2 unless it
/// says otherwise, so that is the divisor back to a standard uncertainty.
const CERTIFICATE_COVERAGE: f64 = 2.0;

type Inputs = BTreeMap<String, f64>;

#[derive(Debug)]
pub struct Budget {
    pub quantity_kind: &'static str,
    pub components: Vec<UncertaintyComponent>,
    pub combined: f64,
    pub effective_dof: f64,
    pub coverage_factor: f64,
    pub coverage_probability: f64,
    pub expanded: f64,
    pub lower_bound: f64,
    pub upper_bound: f64,
}

#[derive(Debug)]
pub struct TypeAFit {
    pub standard_uncertainty: f64,
    pub degrees_of_freedom: f64,
    pub replicate_count: usize,
    pub sufficient: bool,
}

/// Converts a half-width limit into a standard uncertainty.
///
/// A limit known only as "±a, anywhere within" is rectangular, whose variance is
/// a²/3; a triangular limit gives a²/6. A normal limit is already an expanded
/// uncertainty, so its divisor is the coverage factor it was quoted at.
fn divisor(distribution: &str, coverage: f64) -> f64 {
    match distribution {
        DIST_RECTANGULAR => 3.0f64.sqrt(),
        DIST_TRIANGULAR => 6.0f64.sqrt(),
        DIST_NORMAL if coverage > 0.0 => coverage,
        _ => 1.0,
    }
}

fn component(
    name: &str,
    kind: &str,
    distribution: &str,
    coverage: f64,
    half_width: Option<f64>,
    sensitivity: f64,
    degrees_of_freedom: f64,
) -> Option<UncertaintyComponent> {
    let half_width = half_width?;
    if !sensitivity.is_finite() || sensitivity == 0.0 {
        return None;
    }
    Some(UncertaintyComponent {
        name: name.to_string(),
        kind: kind.to_string(),
        distribution: distribution.to_string(),
        value: half_width,
        sensitivity,
        standard_uncertainty: (half_width / divisor(distribution, coverage)) * sensitivity.abs(),
        degrees_of_freedom: finite_dof(degrees_of_freedom),
    })
}

/// Maps the internal infinite degrees of freedom onto the wire's `None`. The
/// two mean the same thing; only `None` survives JSON.
fn finite_dof(dof: f64) -> Option<f64> {
    dof.is_finite().then_some(dof)
}

fn rectangular(name: &str, half_width: Option<f64>, sensitivity: f64) -> Option<UncertaintyComponent> {
    component(
        name,
        TYPE_B,
        DIST_RECTANGULAR,
        0.0,
        half_width,
        sensitivity,
        f64::INFINITY,
    )
}

/// Reads a half-width limit from the model inputs. The sign of a limit carries
/// no information — a ±0.2 °C deviation widens the interval either way — so it
/// is taken as a magnitude, and an absent or zero input contributes nothing.
fn limit(inputs: &Inputs, key: &str) -> Option<f64> {
    inputs
        .get(key)
        .copied()
        .filter(|v| v.is_finite() && *v != 0.0)
        .map(f64::abs)
}

/// A resolution of r means a reading is only known to lie within ±r/2 of what
/// the display shows.
fn resolution_half_width(inputs: &Inputs) -> Option<f64> {
    limit(inputs, "resolution").map(|r| r / 2.0)
}

/// Degrees of freedom for a Type A term. An unstated count cannot be fed to
/// Welch-Satterthwaite, so the term is treated as well determined rather than
/// having a degrees-of-freedom figure invented for it.
fn stated_dof(inputs: &Inputs, key: &str) -> f64 {
    match inputs.get(key).copied() {
        Some(d) if d.is_finite() && d > 0.0 => d,
        _ => f64::INFINITY,
    }
}

fn repeatability_and_calibration(inputs: &Inputs, out: &mut Vec<UncertaintyComponent>) {
    out.extend(component(
        "repeatability",
        TYPE_A,
        DIST_NORMAL,
        1.0,
        limit(inputs, "repeatability_sd"),
        1.0,
        stated_dof(inputs, "repeatability_dof"),
    ));
    out.extend(component(
        "calibration_uncertainty",
        TYPE_B,
        DIST_NORMAL,
        CERTIFICATE_COVERAGE,
        limit(inputs, "calibration_uncertainty"),
        1.0,
        f64::INFINITY,
    ));
    out.extend(rectangular("resolution", resolution_half_width(inputs), 1.0));
}

fn build_components(
    model_id: &str,
    measured_value: f64,
    inputs: &Inputs,
) -> MlResult<(&'static str, Vec<UncertaintyComponent>, &'static [&'static str])> {
    let mut out = Vec::new();
    match model_id {
        MODEL_VOLUME_FLOWMETER => {
            out.extend(rectangular(
                "max_permissible_error",
                limit(inputs, "max_permissible_error"),
                1.0,
            ));
            out.extend(rectangular("resolution", resolution_half_width(inputs), 1.0));
            out.extend(rectangular(
                "temperature_expansion",
                limit(inputs, "temperature_deviation_c").map(|d| d * MILK_VOLUMETRIC_EXPANSION_PER_C),
                measured_value,
            ));
            let drift = limit(inputs, "days_since_verification")
                .zip(limit(inputs, "drift_per_day"))
                .map(|(days, per_day)| days * per_day);
            out.extend(rectangular("drift_since_verification", drift, 1.0));
            Ok((
                "VOLUME_LITRES",
                out,
                &[
                    "max_permissible_error",
                    "resolution",
                    "temperature_deviation_c",
                    "days_since_verification",
                    "drift_per_day",
                ],
            ))
        }
        MODEL_FAT_GERBER => {
            repeatability_and_calibration(inputs, &mut out);
            Ok((
                "FAT_PERCENT",
                out,
                &["repeatability_sd", "calibration_uncertainty", "resolution"],
            ))
        }
        MODEL_SNF_LACTOMETER => {
            repeatability_and_calibration(inputs, &mut out);
            out.extend(rectangular(
                "temperature_correction",
                limit(inputs, "temperature_correction"),
                1.0,
            ));
            Ok((
                "SNF_PERCENT",
                out,
                &[
                    "repeatability_sd",
                    "calibration_uncertainty",
                    "resolution",
                    "temperature_correction",
                ],
            ))
        }
        MODEL_MASS_PLATFORM_SCALE => {
            out.extend(rectangular(
                "max_permissible_error",
                limit(inputs, "max_permissible_error"),
                1.0,
            ));
            out.extend(rectangular("resolution", resolution_half_width(inputs), 1.0));
            out.extend(rectangular("eccentricity", limit(inputs, "eccentricity"), 1.0));
            out.extend(rectangular("buoyancy", limit(inputs, "buoyancy"), 1.0));
            Ok((
                "MASS_KG",
                out,
                &["max_permissible_error", "resolution", "eccentricity", "buoyancy"],
            ))
        }
        other => Err(MlError::NotFound(format!(
            "unknown uncertainty_model_id {other}; registered models are {}",
            MODEL_IDS.join(", ")
        ))),
    }
}

/// Root-sum-square of the component standard uncertainties.
pub fn combined_standard_uncertainty(components: &[UncertaintyComponent]) -> f64 {
    let squares: Vec<f64> = components
        .iter()
        .map(|c| c.standard_uncertainty * c.standard_uncertainty)
        .collect();
    compensated_sum(&squares).sqrt()
}

pub fn estimate(req: &EstimateUncertaintyRequest) -> MlResult<Budget> {
    if !req.measured_value.is_finite() || req.measured_value < 0.0 {
        return Err(MlError::invalid(format!(
            "measured_value must be a finite non-negative quantity, got {}",
            req.measured_value
        )));
    }

    let probability = if req.coverage_probability == 0.0 {
        DEFAULT_COVERAGE_PROBABILITY
    } else {
        req.coverage_probability
    };
    if !(probability > 0.0 && probability < 1.0) {
        return Err(MlError::invalid(format!(
            "coverage_probability must lie in (0, 1), got {}",
            req.coverage_probability
        )));
    }

    let (quantity_kind, components, needed) =
        build_components(&req.uncertainty_model_id, req.measured_value, &req.inputs)?;

    // A model evaluates one quantity. If the caller names a different one its
    // request and its model disagree, and guessing which is right would attach
    // a fat-percent budget to a volume reading.
    if !req.quantity_kind.is_empty() && req.quantity_kind != quantity_kind {
        return Err(MlError::invalid(format!(
            "uncertainty model {} evaluates {quantity_kind}, not {}",
            req.uncertainty_model_id, req.quantity_kind
        )));
    }

    if components.is_empty() {
        // A zero combined uncertainty would assert a perfect measurement. The
        // caller supplied nothing wrong, only nothing usable, so this is
        // insufficient evidence and Go falls back to its deterministic path.
        return Err(MlError::insufficient(format!(
            "uncertainty model {} was given no usable inputs; supply at least one of {}",
            req.uncertainty_model_id,
            needed.join(", ")
        )));
    }

    let combined = combined_standard_uncertainty(&components);
    let dof_pairs: Vec<(f64, f64)> = components
        .iter()
        .map(|c| (c.standard_uncertainty, c.degrees_of_freedom.unwrap_or(f64::INFINITY)))
        .collect();
    let effective_dof = welch_satterthwaite(combined, &dof_pairs);
    let k = coverage_factor(effective_dof, probability);
    let expanded = k * combined;

    Ok(Budget {
        quantity_kind,
        components,
        combined,
        effective_dof,
        coverage_factor: k,
        coverage_probability: probability,
        expanded,
        lower_bound: req.measured_value - expanded,
        upper_bound: req.measured_value + expanded,
    })
}

/// Type A evaluation from repeated observations under repeatability conditions.
pub fn fit_type_a(replicates: &[SeriesPoint]) -> MlResult<TypeAFit> {
    let n = replicates.len();
    if n < MIN_REPLICATES {
        return Err(MlError::insufficient(format!(
            "{n} replicates were supplied; the Type A evaluation needs at least {MIN_REPLICATES}"
        )));
    }

    let values: Vec<f64> = replicates.iter().map(|p| p.value).collect();
    // The quantity reported is the standard uncertainty of the *mean* of the
    // replicates, s/sqrt(n), not the sample standard deviation: the estimate the
    // budget carries forward is the mean, and it is better determined than any
    // single reading.
    let standard_uncertainty = std_error_of_mean(&values).ok_or_else(|| {
        MlError::unprocessable("replicate values have no computable dispersion".to_string())
    })?;

    Ok(TypeAFit {
        standard_uncertainty,
        degrees_of_freedom: (n - 1) as f64,
        replicate_count: n,
        sufficient: n >= SUFFICIENT_REPLICATES,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn sample_std_dev(points: &[SeriesPoint]) -> f64 {
        let values: Vec<f64> = points.iter().map(|p| p.value).collect();
        mlcore::stats::std_dev(&values).unwrap()
    }

    fn request(model_id: &str, measured_value: f64, inputs: &[(&str, f64)]) -> EstimateUncertaintyRequest {
        EstimateUncertaintyRequest {
            tenant_id: "t".to_string(),
            uncertainty_model_id: model_id.to_string(),
            quantity_kind: String::new(),
            measured_value,
            unit: String::new(),
            inputs: inputs.iter().map(|(k, v)| (k.to_string(), *v)).collect(),
            coverage_probability: 0.0,
        }
    }

    fn fixed(u: f64) -> UncertaintyComponent {
        UncertaintyComponent {
            name: "fixed".to_string(),
            kind: TYPE_B.to_string(),
            distribution: DIST_RECTANGULAR.to_string(),
            value: u,
            sensitivity: 1.0,
            standard_uncertainty: u,
            degrees_of_freedom: None,
        }
    }

    fn replicates(values: &[f64]) -> Vec<SeriesPoint> {
        values
            .iter()
            .enumerate()
            .map(|(i, v)| SeriesPoint {
                observation_id: format!("r{i}"),
                valid_at: "2026-01-01T00:00:00Z".to_string(),
                value: *v,
                uncertainty: 0.0,
            })
            .collect()
    }

    #[test]
    fn rectangular_limit_divides_by_root_three() {
        let b = estimate(&request(
            MODEL_MASS_PLATFORM_SCALE,
            100.0,
            &[("max_permissible_error", 0.3)],
        ))
        .unwrap();
        assert_eq!(b.components.len(), 1);
        assert!(
            (b.components[0].standard_uncertainty - 0.173_205_080_756_887_73).abs() < 1e-12,
            "got {}",
            b.components[0].standard_uncertainty
        );
        assert_eq!(b.quantity_kind, "MASS_KG");
    }

    #[test]
    fn triangular_limit_divides_by_root_six() {
        assert!((divisor(DIST_TRIANGULAR, 0.0) - 6.0f64.sqrt()).abs() < 1e-15);
        assert!((divisor(DIST_RECTANGULAR, 0.0) - 3.0f64.sqrt()).abs() < 1e-15);
        assert_eq!(divisor(DIST_NORMAL, 2.0), 2.0);
        assert_eq!(divisor(DIST_NORMAL, 0.0), 1.0);
    }

    #[test]
    fn components_combine_in_quadrature() {
        let combined = combined_standard_uncertainty(&[fixed(0.3), fixed(0.4)]);
        assert!((combined - 0.5).abs() < 1e-12, "got {combined}");
    }

    #[test]
    fn a_pure_type_b_budget_uses_the_normal_coverage_factor() {
        let b = estimate(&request(
            MODEL_MASS_PLATFORM_SCALE,
            250.0,
            &[
                ("max_permissible_error", 0.05),
                ("resolution", 0.02),
                ("eccentricity", 0.03),
                ("buoyancy", 0.01),
            ],
        ))
        .unwrap();
        assert_eq!(b.components.len(), 4);
        assert!(b.components.iter().all(|c| c.kind == TYPE_B));
        assert!(
            b.effective_dof.is_infinite(),
            "type B only budget should have infinite effective dof, got {}",
            b.effective_dof
        );
        assert!((b.coverage_factor - 1.96).abs() < 1e-9);
    }

    #[test]
    fn a_type_a_component_with_few_dof_widens_the_coverage_factor() {
        let type_b_only = estimate(&request(MODEL_FAT_GERBER, 4.0, &[("resolution", 0.02)])).unwrap();
        assert!((type_b_only.coverage_factor - 1.96).abs() < 1e-9);

        let with_type_a = estimate(&request(
            MODEL_FAT_GERBER,
            4.0,
            &[("resolution", 0.02), ("repeatability_sd", 0.05), ("repeatability_dof", 4.0)],
        ))
        .unwrap();
        assert!(with_type_a.effective_dof.is_finite());
        assert!(
            with_type_a.coverage_factor > 1.96,
            "expected k above the normal quantile, got {}",
            with_type_a.coverage_factor
        );
        assert!(with_type_a.components.iter().any(|c| c.kind == TYPE_A));
    }

    #[test]
    fn the_expanded_interval_brackets_the_measured_value_symmetrically() {
        let b = estimate(&request(
            MODEL_VOLUME_FLOWMETER,
            420.0,
            &[("max_permissible_error", 2.1), ("resolution", 0.5)],
        ))
        .unwrap();
        assert!(b.expanded > 0.0);
        assert!((b.expanded - b.coverage_factor * b.combined).abs() < 1e-12);
        assert!((420.0 - b.lower_bound - (b.upper_bound - 420.0)).abs() < 1e-12);
        assert!(b.lower_bound < 420.0 && b.upper_bound > 420.0);
    }

    #[test]
    fn temperature_expansion_scales_with_the_measured_volume() {
        let small = estimate(&request(
            MODEL_VOLUME_FLOWMETER,
            10.0,
            &[("temperature_deviation_c", 4.0)],
        ))
        .unwrap();
        let large = estimate(&request(
            MODEL_VOLUME_FLOWMETER,
            1000.0,
            &[("temperature_deviation_c", 4.0)],
        ))
        .unwrap();
        assert!(large.combined > small.combined * 90.0);
        assert_eq!(small.components[0].sensitivity, 10.0);
    }

    #[test]
    fn drift_needs_both_of_its_inputs() {
        let one_only = estimate(&request(
            MODEL_VOLUME_FLOWMETER,
            100.0,
            &[("max_permissible_error", 1.0), ("days_since_verification", 200.0)],
        ))
        .unwrap();
        assert!(!one_only.components.iter().any(|c| c.name == "drift_since_verification"));

        let both = estimate(&request(
            MODEL_VOLUME_FLOWMETER,
            100.0,
            &[
                ("max_permissible_error", 1.0),
                ("days_since_verification", 200.0),
                ("drift_per_day", 0.002),
            ],
        ))
        .unwrap();
        let drift = both
            .components
            .iter()
            .find(|c| c.name == "drift_since_verification")
            .unwrap();
        assert!((drift.value - 0.4).abs() < 1e-12);
        assert!(both.combined > one_only.combined);
    }

    #[test]
    fn a_model_with_no_usable_inputs_is_insufficient_not_zero() {
        let err = estimate(&request(MODEL_MASS_PLATFORM_SCALE, 100.0, &[])).unwrap_err();
        assert!(
            matches!(err, MlError::InsufficientEvidence(_)),
            "an empty budget must never be reported as a zero uncertainty, got {err:?}"
        );
        assert!(err.to_string().contains("max_permissible_error"));
    }

    #[test]
    fn an_unknown_model_id_is_not_found() {
        let err = estimate(&request("milk.colour.eyeball.v1", 1.0, &[("resolution", 0.1)])).unwrap_err();
        assert!(matches!(err, MlError::NotFound(_)), "got {err:?}");
        assert!(err.to_string().contains(MODEL_FAT_GERBER));
    }

    #[test]
    fn a_quantity_kind_that_contradicts_the_model_is_rejected() {
        let mut req = request(MODEL_FAT_GERBER, 4.0, &[("resolution", 0.02)]);
        req.quantity_kind = "FAT_PERCENT".to_string();
        assert!(estimate(&req).is_ok());
        req.quantity_kind = "VOLUME_LITRES".to_string();
        let err = estimate(&req).unwrap_err();
        assert!(matches!(err, MlError::InvalidArgument(_)), "got {err:?}");
    }

    #[test]
    fn a_negative_measured_value_is_rejected() {
        let err = estimate(&request(MODEL_MASS_PLATFORM_SCALE, -1.0, &[("resolution", 0.1)])).unwrap_err();
        assert!(matches!(err, MlError::InvalidArgument(_)), "got {err:?}");
    }

    #[test]
    fn a_coverage_probability_outside_the_unit_interval_is_rejected() {
        let mut req = request(MODEL_MASS_PLATFORM_SCALE, 10.0, &[("resolution", 0.1)]);
        req.coverage_probability = 1.4;
        assert!(matches!(estimate(&req).unwrap_err(), MlError::InvalidArgument(_)));
        req.coverage_probability = -0.2;
        assert!(matches!(estimate(&req).unwrap_err(), MlError::InvalidArgument(_)));
        req.coverage_probability = 0.0;
        assert_eq!(estimate(&req).unwrap().coverage_probability, 0.95);
    }

    #[test]
    fn a_larger_permissible_error_widens_the_expanded_uncertainty() {
        let tight = estimate(&request(
            MODEL_MASS_PLATFORM_SCALE,
            500.0,
            &[("max_permissible_error", 0.05), ("resolution", 0.02)],
        ))
        .unwrap();
        let loose = estimate(&request(
            MODEL_MASS_PLATFORM_SCALE,
            500.0,
            &[("max_permissible_error", 0.50), ("resolution", 0.02)],
        ))
        .unwrap();
        assert!(
            loose.expanded > tight.expanded,
            "a worse instrument class must widen the interval: {} <= {}",
            loose.expanded,
            tight.expanded
        );
    }

    #[test]
    fn a_certificate_uncertainty_is_divided_by_its_stated_coverage() {
        let b = estimate(&request(MODEL_FAT_GERBER, 4.0, &[("calibration_uncertainty", 0.08)])).unwrap();
        assert_eq!(b.components.len(), 1);
        assert!((b.components[0].standard_uncertainty - 0.04).abs() < 1e-15);
    }

    #[test]
    fn the_snf_model_adds_a_temperature_correction() {
        let b = estimate(&request(
            MODEL_SNF_LACTOMETER,
            8.5,
            &[("resolution", 0.01), ("temperature_correction", 0.06)],
        ))
        .unwrap();
        assert_eq!(b.quantity_kind, "SNF_PERCENT");
        assert!(b.components.iter().any(|c| c.name == "temperature_correction"));
    }

    #[test]
    fn three_replicates_are_insufficient_for_a_type_a_fit() {
        let err = fit_type_a(&replicates(&[4.01, 4.03, 3.99])).unwrap_err();
        assert!(matches!(err, MlError::InsufficientEvidence(_)), "got {err:?}");
        assert!(err.to_string().contains('3'));
        assert!(err.to_string().contains('6'));
    }

    #[test]
    fn twelve_replicates_give_a_sufficient_fit_below_the_sample_dispersion() {
        let values = [
            4.01, 4.03, 3.99, 4.02, 4.00, 3.98, 4.04, 4.01, 3.97, 4.02, 4.00, 4.03,
        ];
        let points = replicates(&values);
        let fit = fit_type_a(&points).unwrap();
        assert!(fit.sufficient);
        assert_eq!(fit.replicate_count, 12);
        assert_eq!(fit.degrees_of_freedom, 11.0);
        let sd = sample_std_dev(&points);
        assert!(
            fit.standard_uncertainty < sd,
            "the mean must be better determined than a single reading: {} >= {sd}",
            fit.standard_uncertainty
        );
        assert!((fit.standard_uncertainty - sd / 12.0f64.sqrt()).abs() < 1e-15);
    }

    #[test]
    fn six_replicates_fit_but_are_not_marked_sufficient() {
        let fit = fit_type_a(&replicates(&[4.01, 4.03, 3.99, 4.02, 4.00, 3.98])).unwrap();
        assert!(!fit.sufficient);
        assert_eq!(fit.degrees_of_freedom, 5.0);
        assert!(fit.standard_uncertainty > 0.0);
    }
}
