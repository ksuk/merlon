import { Link } from "react-router-dom"

export function NotFoundPage() {
  return (
    <div className="flex min-h-[400px] items-center justify-center">
      <div className="text-center">
        <p className="text-6xl font-bold text-muted-foreground/30">404</p>
        <h2 className="mt-4 text-lg font-semibold">ページが見つかりません</h2>
        <p className="mt-2 text-sm text-muted-foreground">
          指定されたURLのページは存在しません。
        </p>
        <Link
          to="/"
          className="mt-6 inline-block rounded-md bg-primary px-4 py-2 text-sm text-primary-foreground hover:bg-primary/90"
        >
          ダッシュボードに戻る
        </Link>
      </div>
    </div>
  )
}
