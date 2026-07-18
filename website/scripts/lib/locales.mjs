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

    // Category labels
    categoryApiReference: "API Reference",
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

    schemaColumn: "Schema",
    titleColumn: "Title",
    descriptionColumn: "Description",

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

    categoryApiReference: "APIリファレンス",
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

    schemaColumn: "スキーマ",
    titleColumn: "タイトル",
    descriptionColumn: "説明",

    openapiVersion: "OpenAPIバージョン",
    apiVersion: "APIバージョン",
    basePath: "ベースパス",

    noFields: "_フィールドはありません。_",
  },
};

export const LOCALE_KEYS = Object.keys(LOCALES);
