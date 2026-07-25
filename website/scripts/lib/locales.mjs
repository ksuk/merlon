// Shared bilingual string dictionary for the docs reference generators
// (generate-schema-docs.mjs, generate-openapi-page.mjs).
//
// Only structural/boilerplate text is localized here (headings, table
// headers, generated-page intro sentences, category labels). Content that
// comes from source code/comments/schemas (descriptions, field names, JSON
// dumps) is always emitted as-is, in whichever language the source uses.

export const LOCALES = {
  en: {
    // Common section headings
    overview: "Overview",
    properties: "Properties",
    constraints: "Constraints",
    services: "Services",
    messages: "Messages",
    enums: "Enums",
    fields: "Fields",
    fullSchema: "Full schema",
    scalarValueTypes: "Scalar Value Types",
    endpointsByMethod: "Endpoints by method",
    fullSpecification: "Full specification",

    // Table headers
    field: "Field",
    value: "Value",
    name: "Name",
    type: "Type",
    required: "Required",
    default: "Default",
    description: "Description",
    endpoints: "Endpoints",
    constraint: "Constraint",
    method: "Method",
    count: "Count",
    schema: "Schema",
    title: "Title",
    package: "Package",
    label: "Label",
    request: "Request",
    response: "Response",
    value_: "Value",
    number: "Number",

    // Category labels. This category holds both the REST API reference and
    // the rule configuration schemas, so it is labelled "Reference" rather
    // than "API Reference" — the latter made the schema pages' `v1`/`v2`
    // suffixes read as versions of the HTTP API.
    categoryApiReference: "Reference",
    categoryRuleSchemas: "Rule Schemas",
    categoryRestApiReference: "REST API Reference",

    // Generated-page intro sentences
    schemaIndexIntro:
      "Merlon's CDD scoring weights, country risk tables, and transaction-monitoring " +
      "scenarios are all expressed as JSON documents validated against the schemas " +
      "below (see `content/schema/` in the repository). These pages are generated " +
      "automatically from those JSON Schema files; do not edit them directly.",
    openapiIntro:
      "This page describes the Go API's REST surface, exported directly from its " +
      "route definitions as an [OpenAPI 3.0](https://spec.openapis.org/oas/v3.0.3) " +
      "document. It is generated automatically (`make generate-openapi`); do not " +
      "edit it directly.",
    openapiFullSpec: (link) =>
      "The complete machine-readable spec is available at " +
      `[\`openapi.json\`](${link}). Load it into any OpenAPI-compatible ` +
      "tool (Swagger UI, Postman, Insomnia, `openapi-generator`, ...) to " +
      "explore or exercise the API interactively.",

    ruleDefinitionSchemasTitle: "Rule Definition Schemas",
    restApiReferenceTitle: "REST API Reference",
    ruleSchemasIndexTitle: "Rule Schemas",

    releaseNotesTitle: "Release Notes",
    releaseNotesIntro:
      "Notable changes in each version, generated from `CHANGELOG.md` in the " +
      "repository. The same file produces the notes attached to each GitHub " +
      "release, so this page and the published releases always match. Before " +
      "upgrading, read the [Upgrade Runbook](operations/upgrade.md).",
    releaseNotesEmpty: "_No changes have been recorded yet._",

    schemaColumn: "Schema",
    titleColumn: "Title",
    descriptionColumn: "Description",

    // Display titles, keyed by schema file name. These override the schema's
    // own `title` for presentation only; the JSON Schema documents under
    // content/schema/ are contract metadata and are never rewritten.
    //
    // tm_scenario_v2 deliberately drops the "v2" suffix: it is the current
    // TM scenario schema, and showing "…Definition v2" in the sidebar next to
    // the REST API pages made it read as a second version of the HTTP API.
    // The schema version itself is stated in the page body and in the
    // `schema_version` field documentation.
    schemaDisplayTitles: {
      tm_scenario_v2: "TM Scenario Definition",
      tm_scenario_v1: "TM Scenario Definition (v1, legacy)",
    },
    // Presentation descriptions for schemas whose JSON Schema has no
    // `description` of its own, so the index table has no empty cells.
    schemaDescriptions: {
      country_risk_v1:
        "Schema for country risk score tables, including the effective date " +
        "and the default score for unlisted countries",
      tm_scenario_v2:
        "Current schema for transaction monitoring scenario definitions, with " +
        "per-customer-type and per-risk-tier thresholds",
    },

    schemaNotApiVersion:
      ":::note\n\n" +
      "The `v1`/`v2` suffixes on this page refer to **rule configuration file " +
      "schema versions**, not to versions of the REST API. The REST API is " +
      "versioned separately under its `/api/v1` path prefix; see the " +
      "[REST API Reference](../openapi.md).\n\n" +
      ":::",
    schemaLegacyNote: (link, title) =>
      `A superseded schema, [${title}](${link}), is still accepted by the ` +
      "engine for backward compatibility but is not listed above. No bundled " +
      "content uses it, and new rule files should not be authored against it.",
    schemaLegacyBanner:
      ":::warning[Legacy schema]\n\n" +
      "This schema is superseded and is documented only for backward " +
      "compatibility. None of the bundled sample content uses it, and new " +
      "scenarios should be authored against the current " +
      "[TM Scenario Definition](./tm_scenario_v2.md) schema.\n\n" +
      "The engine continues to accept this format, and the schema file is " +
      "maintained until 2027-07-04 per ADR-0006 (tm_scenario_v2 schema and v1 " +
      "dual support).\n\n" +
      ":::",

    openapiVersion: "OpenAPI version",
    apiVersion: "API version",
    basePath: "Base path",

    noFields: "_No fields._",
  },

  ja: {
    overview: "概要",
    properties: "プロパティ",
    constraints: "制約",
    services: "サービス",
    messages: "メッセージ",
    enums: "列挙型",
    fields: "フィールド",
    fullSchema: "完全なスキーマ",
    scalarValueTypes: "スカラー値型",
    endpointsByMethod: "メソッド別エンドポイント",
    fullSpecification: "完全な仕様",

    field: "フィールド",
    value: "値",
    name: "名前",
    type: "型",
    required: "必須",
    default: "デフォルト",
    description: "説明",
    endpoints: "エンドポイント",
    constraint: "制約",
    method: "メソッド",
    count: "件数",
    schema: "スキーマ",
    title: "タイトル",
    package: "パッケージ",
    label: "ラベル",
    request: "リクエスト",
    response: "レスポンス",
    value_: "値",
    number: "番号",

    categoryApiReference: "リファレンス",
    categoryRuleSchemas: "ルールスキーマ",
    categoryRestApiReference: "REST APIリファレンス",

    schemaIndexIntro:
      "Merlonの CDD スコアリングの重み、カントリーリスクテーブル、トランザクション" +
      "モニタリングのシナリオは、いずれもリポジトリの `content/schema/` にある " +
      "JSON Schema で検証される JSON ドキュメントとして表現されています。このページは" +
      "それらの JSON Schema ファイルから自動生成されています。直接編集しないでください。",
    openapiIntro:
      "このページは、Go API の REST サーフェスをそのルート定義から直接エクスポートした " +
      "[OpenAPI 3.0](https://spec.openapis.org/oas/v3.0.3) ドキュメントです。" +
      "`make generate-openapi` により自動生成されています。直接編集しないでください。",
    openapiFullSpec: (link) =>
      "完全な機械可読仕様は " +
      `[\`openapi.json\`](${link}) から入手できます。Swagger UI、Postman、Insomnia、` +
      "`openapi-generator` などの OpenAPI 対応ツールに読み込んで、API を対話的に" +
      "探索・実行できます。",

    ruleDefinitionSchemasTitle: "ルール定義スキーマ",
    restApiReferenceTitle: "REST APIリファレンス",
    ruleSchemasIndexTitle: "ルールスキーマ",

    releaseNotesTitle: "リリースノート",
    releaseNotesIntro:
      "各バージョンの主な変更点です。リポジトリの `CHANGELOG.md` から自動生成" +
      "されています。同じファイルから GitHub リリースのノートも生成されるため、" +
      "このページと公開されたリリースの内容は常に一致します。" +
      "アップグレード前に[アップグレード手順](operations/upgrade.md)を参照してください。",
    releaseNotesEmpty: "_まだ変更は記録されていません。_",

    schemaColumn: "スキーマ",
    titleColumn: "タイトル",
    descriptionColumn: "説明",

    // 表示専用のタイトル（content/schema/ の JSON Schema 自体は変更しない）。
    // tm_scenario_v2 から "v2" を外しているのは、現行の TM シナリオスキーマで
    // ありながら REST API の第2版と誤読されていたため。
    schemaDisplayTitles: {
      cdd_weight_v1: "CDD 重み定義",
      country_risk_v1: "カントリーリスクテーブル",
      tm_scenario_v2: "TM シナリオ定義",
      tm_scenario_v1: "TM シナリオ定義（v1・レガシー）",
    },
    schemaDescriptions: {
      cdd_weight_v1: "顧客リスク評価（CDD）の重み定義スキーマ",
      country_risk_v1:
        "カントリーリスクスコア表のスキーマ。適用開始日と、表に未掲載の国に" +
        "適用されるデフォルトスコアを含む",
      tm_scenario_v2:
        "取引モニタリング（TM）シナリオ定義の現行スキーマ。顧客種別別・" +
        "リスクティア別の閾値に対応",
      tm_scenario_v1: "取引モニタリング（TM）シナリオ定義スキーマ",
    },

    schemaNotApiVersion:
      ":::note\n\n" +
      "このページの `v1`/`v2` という接尾辞は、**ルール設定ファイルのスキーマ" +
      "バージョン**を指すものであり、REST API のバージョンではありません。" +
      "REST API のバージョンはパス接頭辞 `/api/v1` で別途管理されています" +
      "（[REST APIリファレンス](../openapi.md)を参照）。\n\n" +
      ":::",
    schemaLegacyNote: (link, title) =>
      `後方互換のためエンジンが受理し続けている旧スキーマ [${title}](${link}) が` +
      "ありますが、上表には掲載していません。同梱コンテンツで使用しているものは" +
      "なく、新規のルールファイルをこの形式で作成しないでください。",
    schemaLegacyBanner:
      ":::warning[レガシースキーマ]\n\n" +
      "このスキーマは後継版に置き換えられており、後方互換のためだけに記載しています。" +
      "同梱のサンプルコンテンツで使用しているものはなく、新規シナリオは現行の" +
      "[TM シナリオ定義](./tm_scenario_v2.md)スキーマで作成してください。\n\n" +
      "エンジンはこの形式を引き続き受理し、スキーマファイルは ADR-0006" +
      "（tm_scenario_v2 スキーマと v1 デュアルサポート）に基づき 2027-07-04 まで" +
      "維持されます。\n\n" +
      ":::",

    openapiVersion: "OpenAPIバージョン",
    apiVersion: "APIバージョン",
    basePath: "ベースパス",

    noFields: "_フィールドはありません。_",
  },
};

export const LOCALE_KEYS = Object.keys(LOCALES);
