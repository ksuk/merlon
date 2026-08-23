# 共有 outcome matcher

`api/internal/outcome` パッケージは、backtest の outcome 分析と known-matter
coverage が共有する決定的なマッチング契約です。

## 契約

- 主単位は alert です。customer-period 表示は集計であり、別のマッチング実装ではありません。
- candidate と reference は同一 customer でなければなりません。backtest は同一 scenario も要求し、coverage は scenario 横断の union を許可します。
- 両方に transaction 集合があれば Jaccard 類似度を使います。集合がない、または区間の方が強い場合は検知区間の overlap coefficient を使います。`0.50` は含みます。
- assignment は one-to-one で、score 降順、時間差昇順、candidate ID と reference ID の順に決定します。
- label は `TP`、`FP`、`unlabeled`、`unevaluable` です。unlabeled と unevaluable は監査用に残し、rate の分母から除外します。event 時点の score history が欠ける場合は unevaluable とし、現在の customer tier へ fallback しません。

各結果には `matcher_version`、assumptions、as-of snapshot、source provenance、rate の分母を含めます。
