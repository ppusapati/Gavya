//! Anomaly detection over a single subject's measurement history.
//!
//! The method is deliberately simple and inspectable rather than a learned
//! black box: a robust z-score against a leave-one-out baseline, with the
//! tolerance band widened by the measurement's own stated uncertainty. Every
//! score a reviewer sees must be one they can recompute by hand, because a
//! flag here can hold up a producer's payment.

use mlcore::stats::{mad, median, robust_z, std_dev};

use crate::contract::{AnomalyScore, SeriesPoint};

/// Below this many baseline points there is no defensible notion of "normal".
pub const MIN_BASELINE: usize = 5;

pub const DEFAULT_SENSITIVITY: f64 = 3.5;

pub const METHOD_ROBUST_Z: &str = "robust_z_mad";
pub const METHOD_DEGENERATE: &str = "degenerate_baseline";

/// Scores one candidate against a baseline that excludes it.
///
/// Leaving the candidate out matters: with a short series, including a gross
/// outlier in its own baseline inflates the MAD enough to mask the very point
/// being tested.
pub fn score_against(candidate: &SeriesPoint, baseline: &[f64], sensitivity: f64) -> AnomalyScore {
    let threshold = if sensitivity > 0.0 {
        sensitivity
    } else {
        DEFAULT_SENSITIVITY
    };

    let centre = median(baseline).unwrap_or(candidate.value);

    let Some(z) = robust_z(candidate.value, baseline) else {
        // No dispersion in the baseline. Every historical reading is identical,
        // which is itself suspicious for a physical measurement, so a departure
        // is reported without a finite score rather than silently passed.
        let differs = (candidate.value - centre).abs() > f64::EPSILON;
        return AnomalyScore {
            observation_id: candidate.observation_id.clone(),
            score: 0.0,
            flagged: differs,
            method: METHOD_DEGENERATE.to_string(),
            expected: centre,
            lower_bound: Some(centre),
            upper_bound: Some(centre),
            explanation: if differs {
                format!(
                    "baseline has no dispersion (every reading {centre}); this reading of {} cannot be scored and needs review",
                    candidate.value
                )
            } else {
                format!("baseline has no dispersion; reading matches the constant baseline of {centre}")
            },
        };
    };

    // Widen the band by the measurement's own standard uncertainty so a less
    // precise instrument is not punished for being less precise.
    let scale = mad(baseline)
        .filter(|m| *m > f64::EPSILON)
        .or_else(|| std_dev(baseline))
        .unwrap_or(0.0);
    let half_width = threshold * scale + candidate.uncertainty;
    let lower = centre - half_width;
    let upper = centre + half_width;

    let flagged = candidate.value < lower || candidate.value > upper;
    let direction = if z >= 0.0 { "above" } else { "below" };

    AnomalyScore {
        observation_id: candidate.observation_id.clone(),
        score: z,
        flagged,
        method: METHOD_ROBUST_Z.to_string(),
        expected: centre,
        lower_bound: Some(lower),
        upper_bound: Some(upper),
        explanation: format!(
            "reading {:.4} is {:.2} robust standard deviations {} the baseline median {:.4}; \
             tolerance band [{:.4}, {:.4}] at sensitivity {:.2}{}",
            candidate.value,
            z.abs(),
            direction,
            centre,
            lower,
            upper,
            threshold,
            if candidate.uncertainty > 0.0 {
                format!(" widened by stated uncertainty {:.4}", candidate.uncertainty)
            } else {
                String::new()
            }
        ),
    }
}

/// Scores every point in a series, each against the others.
pub fn score_series(points: &[SeriesPoint], sensitivity: f64) -> (Vec<AnomalyScore>, bool) {
    // Each point needs MIN_BASELINE others to be scored against.
    if points.len() <= MIN_BASELINE {
        let scores = points
            .iter()
            .map(|p| AnomalyScore {
                observation_id: p.observation_id.clone(),
                score: 0.0,
                flagged: false,
                method: METHOD_DEGENERATE.to_string(),
                expected: p.value,
                lower_bound: None,
                upper_bound: None,
                explanation: format!(
                    "series has {} points; at least {} are required to establish a baseline",
                    points.len(),
                    MIN_BASELINE + 1
                ),
            })
            .collect();
        return (scores, true);
    }

    let scores = points
        .iter()
        .enumerate()
        .map(|(i, candidate)| {
            let baseline: Vec<f64> = points
                .iter()
                .enumerate()
                .filter(|(j, _)| *j != i)
                .map(|(_, p)| p.value)
                .collect();
            score_against(candidate, &baseline, sensitivity)
        })
        .collect();

    (scores, false)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn point(id: &str, value: f64) -> SeriesPoint {
        SeriesPoint {
            observation_id: id.to_string(),
            valid_at: "2026-01-01T00:00:00Z".to_string(),
            value,
            uncertainty: 0.0,
        }
    }

    fn steady_series() -> Vec<SeriesPoint> {
        vec![
            point("a", 12.0),
            point("b", 12.4),
            point("c", 11.8),
            point("d", 12.2),
            point("e", 12.1),
            point("f", 11.9),
            point("g", 12.3),
        ]
    }

    #[test]
    fn steady_series_flags_nothing() {
        let (scores, insufficient) = score_series(&steady_series(), 0.0);
        assert!(!insufficient);
        assert!(
            scores.iter().all(|s| !s.flagged),
            "steady series produced a flag: {:?}",
            scores.iter().filter(|s| s.flagged).collect::<Vec<_>>()
        );
    }

    #[test]
    fn a_single_spike_is_flagged() {
        let mut series = steady_series();
        series.push(point("spike", 95.0));
        let (scores, insufficient) = score_series(&series, 0.0);
        assert!(!insufficient);
        let spike = scores.iter().find(|s| s.observation_id == "spike").unwrap();
        assert!(spike.flagged, "the spike was not flagged: {spike:?}");
        assert_eq!(spike.method, METHOD_ROBUST_Z);
        assert_eq!(scores.iter().filter(|s| s.flagged).count(), 1);
    }

    #[test]
    fn a_drop_to_zero_is_flagged() {
        let mut series = steady_series();
        series.push(point("zero", 0.0));
        let (scores, _) = score_series(&series, 0.0);
        assert!(scores.iter().find(|s| s.observation_id == "zero").unwrap().flagged);
    }

    #[test]
    fn short_series_reports_insufficient_baseline_and_flags_nothing() {
        let series = vec![point("a", 12.0), point("b", 900.0)];
        let (scores, insufficient) = score_series(&series, 0.0);
        assert!(insufficient, "a two-point series must not be treated as a baseline");
        assert!(scores.iter().all(|s| !s.flagged));
    }

    #[test]
    fn stated_uncertainty_widens_the_band() {
        let baseline: Vec<f64> = steady_series().iter().map(|p| p.value).collect();
        let mut candidate = point("wide", 14.0);

        let strict = score_against(&candidate, &baseline, 0.0);
        assert!(strict.flagged, "reading should be out of band without uncertainty");

        candidate.uncertainty = 3.0;
        let tolerant = score_against(&candidate, &baseline, 0.0);
        assert!(
            !tolerant.flagged,
            "a reading within its own stated uncertainty must not be flagged"
        );
        assert!(tolerant.upper_bound.unwrap() > strict.upper_bound.unwrap());
    }

    #[test]
    fn lower_sensitivity_flags_more() {
        let mut series = steady_series();
        series.push(point("mild", 13.4));
        let (strict, _) = score_series(&series, 8.0);
        let (loose, _) = score_series(&series, 1.5);
        let strict_flags = strict.iter().filter(|s| s.flagged).count();
        let loose_flags = loose.iter().filter(|s| s.flagged).count();
        assert!(
            loose_flags >= strict_flags,
            "loosening sensitivity reduced the flag count: {loose_flags} < {strict_flags}"
        );
    }

    #[test]
    fn constant_baseline_is_reported_not_scored() {
        let baseline = vec![10.0; 8];
        let s = score_against(&point("x", 10.5), &baseline, 0.0);
        assert_eq!(s.method, METHOD_DEGENERATE);
        assert!(s.flagged, "a departure from a constant baseline needs review");
    }

    #[test]
    fn leave_one_out_prevents_an_outlier_masking_itself() {
        // Included in its own baseline, this outlier would inflate the scale
        // enough to fall inside the band it created.
        let series = vec![
            point("a", 10.0),
            point("b", 10.1),
            point("c", 9.9),
            point("d", 10.2),
            point("e", 9.8),
            point("f", 10.05),
            point("out", 40.0),
        ];
        let (scores, _) = score_series(&series, 0.0);
        assert!(scores.iter().find(|s| s.observation_id == "out").unwrap().flagged);
    }
}
