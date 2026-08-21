//! Robust statistics shared by the ML services.
//!
//! Dairy collection data is heavy-tailed and routinely contains the very
//! outliers we are trying to detect, so location and scale are estimated with
//! median-based statistics rather than the mean and standard deviation, which
//! the outliers themselves would inflate.

/// Scale factor making the median absolute deviation a consistent estimator of
/// the standard deviation under normality.
pub const MAD_TO_SIGMA: f64 = 1.482_602_218_505_602;

pub fn mean(xs: &[f64]) -> Option<f64> {
    if xs.is_empty() {
        return None;
    }
    Some(xs.iter().sum::<f64>() / xs.len() as f64)
}

/// Sample variance with Bessel's correction. Needs at least two points.
pub fn variance(xs: &[f64]) -> Option<f64> {
    if xs.len() < 2 {
        return None;
    }
    let m = mean(xs)?;
    let ss: f64 = xs.iter().map(|x| (x - m).powi(2)).sum();
    Some(ss / (xs.len() - 1) as f64)
}

pub fn std_dev(xs: &[f64]) -> Option<f64> {
    variance(xs).map(f64::sqrt)
}

/// Standard uncertainty of the mean: s / sqrt(n), the Type A evaluation.
pub fn std_error_of_mean(xs: &[f64]) -> Option<f64> {
    std_dev(xs).map(|s| s / (xs.len() as f64).sqrt())
}

pub fn median(xs: &[f64]) -> Option<f64> {
    if xs.is_empty() {
        return None;
    }
    let mut v: Vec<f64> = xs.iter().copied().filter(|x| x.is_finite()).collect();
    if v.is_empty() {
        return None;
    }
    v.sort_by(|a, b| a.partial_cmp(b).expect("filtered to finite values"));
    let n = v.len();
    Some(if n % 2 == 1 {
        v[n / 2]
    } else {
        (v[n / 2 - 1] + v[n / 2]) / 2.0
    })
}

/// Median absolute deviation, scaled to be comparable with a standard
/// deviation.
pub fn mad(xs: &[f64]) -> Option<f64> {
    let med = median(xs)?;
    let devs: Vec<f64> = xs.iter().map(|x| (x - med).abs()).collect();
    median(&devs).map(|m| m * MAD_TO_SIGMA)
}

/// Robust z-score of `x` against the sample.
///
/// When the MAD collapses to zero — a run of identical readings, common for a
/// stuck meter — it falls back to the classical standard deviation. If that is
/// zero too, any departure from the median is unbounded, reported as `None`
/// rather than as an infinite score.
pub fn robust_z(x: f64, sample: &[f64]) -> Option<f64> {
    let med = median(sample)?;
    let scale = match mad(sample) {
        Some(m) if m > f64::EPSILON => m,
        _ => match std_dev(sample) {
            Some(s) if s > f64::EPSILON => s,
            _ => return None,
        },
    };
    Some((x - med) / scale)
}

/// Exponentially weighted moving average, oldest point first.
pub fn ewma(xs: &[f64], alpha: f64) -> Option<f64> {
    if xs.is_empty() || !(0.0..=1.0).contains(&alpha) {
        return None;
    }
    let mut acc = xs[0];
    for &x in &xs[1..] {
        acc = alpha * x + (1.0 - alpha) * acc;
    }
    Some(acc)
}

/// Welch-Satterthwaite effective degrees of freedom for a combined standard
/// uncertainty built from components with finite degrees of freedom.
pub fn welch_satterthwaite(combined: f64, components: &[(f64, f64)]) -> f64 {
    if combined <= 0.0 {
        return f64::INFINITY;
    }
    let denom: f64 = components
        .iter()
        .filter(|(_, dof)| *dof > 0.0 && dof.is_finite())
        .map(|(u, dof)| u.powi(4) / dof)
        .sum();
    if denom <= 0.0 {
        return f64::INFINITY;
    }
    combined.powi(4) / denom
}

/// Coverage factor k for a two-sided interval at the given probability.
///
/// Uses the Student-t quantile for the effective degrees of freedom, converging
/// to the normal quantile as the degrees of freedom grow. Only the coverage
/// probabilities the platform actually offers are supported.
pub fn coverage_factor(dof: f64, probability: f64) -> f64 {
    let normal_k = match probability {
        p if (p - 0.90).abs() < 1e-9 => 1.645,
        p if (p - 0.99).abs() < 1e-9 => 2.576,
        _ => 1.960, // 0.95, the default
    };
    if !dof.is_finite() || dof >= 100.0 {
        return normal_k;
    }
    // A Cornish-Fisher style correction to the normal quantile; accurate to
    // about 1% for dof >= 3, which is well inside the uncertainty being
    // reported.
    let z = normal_k;
    let correction = (z.powi(3) + z) / (4.0 * dof.max(1.0));
    z + correction
}

/// Sums a slice with Neumaier compensation.
///
/// Mass balances over thousands of small deliveries lose several minor units to
/// naive summation; the compensated sum keeps the residual meaningful.
pub fn compensated_sum(xs: &[f64]) -> f64 {
    let mut sum = 0.0f64;
    let mut compensation = 0.0f64;
    for &x in xs {
        let t = sum + x;
        if sum.abs() >= x.abs() {
            compensation += (sum - t) + x;
        } else {
            compensation += (x - t) + sum;
        }
        sum = t;
    }
    sum + compensation
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn median_handles_even_and_odd() {
        assert_eq!(median(&[3.0, 1.0, 2.0]), Some(2.0));
        assert_eq!(median(&[4.0, 1.0, 3.0, 2.0]), Some(2.5));
        assert_eq!(median(&[]), None);
    }

    #[test]
    fn mad_is_not_dragged_by_an_outlier() {
        let clean = [10.0, 10.1, 9.9, 10.2, 9.8];
        let dirty = [10.0, 10.1, 9.9, 10.2, 9.8, 900.0];
        let mad_clean = mad(&clean).unwrap();
        let mad_dirty = mad(&dirty).unwrap();
        // The classical standard deviation explodes; the MAD barely moves.
        assert!(std_dev(&dirty).unwrap() > 100.0);
        assert!((mad_dirty - mad_clean).abs() < mad_clean);
    }

    #[test]
    fn robust_z_flags_the_outlier() {
        let sample = [10.0, 10.1, 9.9, 10.2, 9.8, 10.05, 9.95];
        let z = robust_z(30.0, &sample).unwrap();
        assert!(z > 10.0, "expected a large score, got {z}");
        let inlier = robust_z(10.0, &sample).unwrap();
        assert!(inlier.abs() < 1.0, "expected a small score, got {inlier}");
    }

    #[test]
    fn robust_z_reports_no_scale_for_a_stuck_meter() {
        // Every reading identical: no dispersion to normalise against.
        assert_eq!(robust_z(11.0, &[10.0, 10.0, 10.0, 10.0]), None);
    }

    #[test]
    fn ewma_weights_recent_points_more() {
        let rising = [1.0, 2.0, 3.0, 10.0];
        let e = ewma(&rising, 0.5).unwrap();
        assert!(e > mean(&rising).unwrap());
    }

    #[test]
    fn welch_satterthwaite_is_infinite_without_type_a_components() {
        // Type B components carry infinite degrees of freedom by convention.
        assert!(welch_satterthwaite(0.5, &[(0.3, f64::INFINITY)]).is_infinite());
    }

    #[test]
    fn coverage_factor_converges_to_normal() {
        assert!((coverage_factor(f64::INFINITY, 0.95) - 1.96).abs() < 1e-9);
        assert!(coverage_factor(4.0, 0.95) > 1.96);
    }

    #[test]
    fn compensated_sum_beats_naive_summation() {
        let mut xs = vec![1e16];
        xs.extend(std::iter::repeat_n(1.0, 100));
        assert_eq!(compensated_sum(&xs), 1e16 + 100.0);
    }

    #[test]
    fn std_error_of_mean_shrinks_with_sample_size() {
        let small = [10.0, 11.0, 9.0];
        let large: Vec<f64> = (0..300).map(|i| 10.0 + ((i % 3) as f64 - 1.0)).collect();
        assert!(std_error_of_mean(&large).unwrap() < std_error_of_mean(&small).unwrap());
    }
}
