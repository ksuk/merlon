fn levenshtein(a: &str, b: &str) -> usize {
    let a_len = a.chars().count();
    let b_len = b.chars().count();

    if a_len == 0 {
        return b_len;
    }
    if b_len == 0 {
        return a_len;
    }

    let a_chars: Vec<char> = a.chars().collect();
    let b_chars: Vec<char> = b.chars().collect();

    let mut prev: Vec<usize> = (0..=b_len).collect();
    let mut curr = vec![0; b_len + 1];

    for i in 1..=a_len {
        curr[0] = i;
        for j in 1..=b_len {
            let cost = if a_chars[i - 1] == b_chars[j - 1] {
                0
            } else {
                1
            };
            curr[j] = (prev[j] + 1)
                .min(curr[j - 1] + 1)
                .min(prev[j - 1] + cost);
        }
        std::mem::swap(&mut prev, &mut curr);
    }

    prev[b_len]
}

pub fn similarity(a: &str, b: &str) -> f64 {
    let a_norm = normalize(a);
    let b_norm = normalize(b);

    if a_norm == b_norm {
        return 1.0;
    }

    let max_len = a_norm.chars().count().max(b_norm.chars().count());
    if max_len == 0 {
        return 1.0;
    }

    let dist = levenshtein(&a_norm, &b_norm);
    1.0 - (dist as f64 / max_len as f64)
}

fn normalize(s: &str) -> String {
    s.to_lowercase()
        .chars()
        .filter(|c| !c.is_ascii_punctuation() || *c == '-')
        .collect::<String>()
        .split_whitespace()
        .collect::<Vec<&str>>()
        .join(" ")
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_exact_match() {
        assert!((similarity("Kim Jong Un", "Kim Jong Un") - 1.0).abs() < f64::EPSILON);
    }

    #[test]
    fn test_case_insensitive() {
        assert!((similarity("kim jong un", "Kim Jong Un") - 1.0).abs() < f64::EPSILON);
    }

    #[test]
    fn test_close_match() {
        let s = similarity("Kim Jong Un", "Kim Jong-Un");
        assert!(s > 0.85, "similarity = {s}");
    }

    #[test]
    fn test_partial_match() {
        let s = similarity("Kim Jong", "Kim Jong Un");
        assert!(s > 0.7, "similarity = {s}");
    }

    #[test]
    fn test_no_match() {
        let s = similarity("John Smith", "田中太郎");
        assert!(s < 0.3, "similarity = {s}");
    }

    #[test]
    fn test_japanese_exact() {
        assert!((similarity("田中太郎", "田中太郎") - 1.0).abs() < f64::EPSILON);
    }

    #[test]
    fn test_japanese_close() {
        let s = similarity("田中太郎", "田中次郎");
        assert!(s > 0.5, "similarity = {s}");
    }

    #[test]
    fn test_empty_strings() {
        assert!((similarity("", "") - 1.0).abs() < f64::EPSILON);
    }

    #[test]
    fn test_one_empty() {
        assert!((similarity("test", "")).abs() < f64::EPSILON);
    }

    #[test]
    fn test_normalize_whitespace() {
        assert!((similarity("Kim  Jong   Un", "Kim Jong Un") - 1.0).abs() < f64::EPSILON);
    }
}
