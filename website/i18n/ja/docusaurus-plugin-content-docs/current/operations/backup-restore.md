---
title: バックアップ・リストア運用手順
---

# バックアップ・リストア運用手順

Merlon のバックアップは **1つではなく2つの成果物**である。PostgreSQL データベースと、暗号鍵リングである。

直接的な PII 顧客属性は、データベース外の `MERLON_ENCRYPTION_KEY_RING` が保持する鍵で保存時に暗号化されている。対応する鍵を伴わないデータベースバックアップは、それらの属性を恒久的に読み取れない状態にする。復旧経路は存在しない。サポート経路も、ベンダー経路も無い。これが本ページで最も重大な事実である。

## バックアップ

```bash
export MERLON_BACKUP_DATABASE_URL='postgres://merlon_backup:...@host:5432/merlon'
export MERLON_ENCRYPTION_KEY_RING='...'
make backup            # または: scripts/backup.sh [出力ディレクトリ]
```

3つのファイルを出力する。

| ファイル | 内容 |
|---|---|
| `merlon-db-<timestamp>.dump` | `pg_dump` custom 形式のデータベースダンプ |
| `merlon-keyring-<timestamp>.env` | 鍵リング（パーミッション `0600`） |
| `merlon-backup-<timestamp>.json` | マニフェスト。両者のタイムスタンプと SHA-256 |

データベース全体のダンプには、migration ledger や sequence state など運用者専用のオブジェクトも含まれる。そのため、`MERLON_BACKUP_DATABASE_URL` には、既存および将来のすべての table／sequence を読み取れる専用 read-only backup role を使用する。スクリプトは serving role や DDL 可能な migration owner へ意図的にフォールバックしない。

### パスワードをプロセス表に出さない

接続 URL は `pg_dump`、`psql`、`pg_restore` にコマンドライン引数として渡される。バックアップやリストアが動作している間、同居する他ユーザー、PID 名前空間を共有するコンテナ、プロセスをサンプリングする各種エージェントは、`ps` や `/proc/<pid>/cmdline` からこれを読み取れる。以下の例が URL 形式を用いているのは簡潔で自己完結しているためであり、自組織が管理するホスト上での対話的な単発実行にはそれが適している。

定期実行される本番バックアップでは、代わりに libpq の環境変数で接続を与え、`MERLON_BACKUP_DATABASE_URL` は設定しない。

```bash
export PGHOST=db.internal PGPORT=5432 PGDATABASE=merlon PGUSER=merlon_backup
export PGPASSWORD="$(read-from-your-secret-store)"   # または ~/.pgpass を使う
make backup
```

`scripts/restore.sh` も同様に、`MERLON_MIGRATION_DATABASE_URL` の代わりにこの形式を受け付ける。`PGSERVICE` エントリや `~/.pgpass`（モード `0600`）でも同等であり、そちらはパスワードを環境変数からも外せる。この方式では両スクリプトとも接続引数を一切渡さない。DSN を組み立て直してコマンドラインに戻すことこそが、回避したい露出そのものだからである。

### backup role のプロビジョニング

認証情報は管理された secret management 手段で作成し、database／role 名を必要に応じて置き換えた上で、次に注記した administrator／object-owner の責務に従って実行する。

```sql
-- database administrator として実行し、認証は別途安全に設定する。
CREATE ROLE merlon_backup
  LOGIN NOINHERIT NOSUPERUSER NOCREATEDB NOCREATEROLE
  NOREPLICATION NOBYPASSRLS;

-- database owner／administrator として database grant を実行する。
GRANT CONNECT ON DATABASE merlon TO merlon_backup;

-- object owner である merlon_migrate として schema/object/default grant を実行する。
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
REVOKE ALL PRIVILEGES ON SCHEMA public FROM merlon_backup;
GRANT USAGE ON SCHEMA public TO merlon_backup;
REVOKE ALL PRIVILEGES ON ALL TABLES IN SCHEMA public FROM merlon_backup;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO merlon_backup;
REVOKE ALL PRIVILEGES ON ALL SEQUENCES IN SCHEMA public FROM merlon_backup;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO merlon_backup;

ALTER DEFAULT PRIVILEGES FOR ROLE merlon_migrate
  REVOKE ALL PRIVILEGES ON SCHEMAS FROM merlon_backup;
ALTER DEFAULT PRIVILEGES FOR ROLE merlon_migrate
  GRANT USAGE ON SCHEMAS TO merlon_backup;
ALTER DEFAULT PRIVILEGES FOR ROLE merlon_migrate
  REVOKE ALL PRIVILEGES ON TABLES FROM merlon_backup;
ALTER DEFAULT PRIVILEGES FOR ROLE merlon_migrate
  GRANT SELECT ON TABLES TO merlon_backup;
ALTER DEFAULT PRIVILEGES FOR ROLE merlon_migrate
  REVOKE ALL PRIVILEGES ON SEQUENCES FROM merlon_backup;
ALTER DEFAULT PRIVILEGES FOR ROLE merlon_migrate
  GRANT SELECT ON SEQUENCES TO merlon_backup;
```

default privilege は現在の database と、object を作成する正確な role の両方にscopeされ、role membership からそのroleのdefaultは適用されない。すべてのtarget databaseとobjectを作成する各roleについて6つの `ALTER DEFAULT PRIVILEGES` を繰り返し、`public` 以外の対応application schemaがある場合はdirect normalizationも繰り返す。role導入後およびrestoreのたびに既存object用grantを再実行する。

table の `INSERT`、`UPDATE`、`DELETE`、`TRUNCATE`、`TRIGGER`、`REFERENCES`、PostgreSQL 18 の `MAINTAIN`、sequence の `USAGE`／`UPDATE`、schema の `CREATE`、role membership、ownership は付与しない。row-level security を導入する場合は完全な dump を明示的にテストし、広範な ownership や DDL 権限を黙って付与するのではなく、必要最小限の RLS 方針をレビューする。

Merlon のこの logical backup は PostgreSQL large object を作成・サポートしない。スクリプトは `pg_largeobject_metadata` を照会し、1つでも存在する場合は `pg_dump` の前に backup artifact の作成を拒否する。組織管理の large object は別の統制された backup 経路へ移すこと。追加した schema と RLS policy も、database object model を変更するたびにテストする。

`MERLON_ENCRYPTION_KEY_RING` が未設定の場合、スクリプトは**実行を拒否する**。データベースのみのバックアップを黙って生成することはしない。暗号化属性を一切保存しないデプロイに限り `--no-keys` を渡すこと。本番データベースでこれが正しいことはない。

出力ディレクトリは `BACKUP_DIR` で指定する（`make backup BACKUP_DIR=/mnt/backups`）。既定は `backups/`。スクリプトが新規作成したdirectoryはmode `0700`にする。既存のdirectoryの権限は設定したまま変更しない。共有マウントやNFS exportに対してgroup accessを黙って剥奪すれば、それを読む他の仕組みが壊れる上に、その原因をこのジョブに結び付ける手がかりも残らないためである。ただしgroup／otherから到達可能な場合は警告する。いずれの場合も、dump、key-ring file、manifestはgroup／other accessなしで作成される。スクリプトは同じdirectory内のhidden temporary fileへ書き込み、manifestを最後に公開する。`pg_dump`が失敗した場合はtemporary dumpを削除し、final-name backupを残さない。

### 保管

**鍵リングは、データベースバックアップとは別の場所に置くこと。** 両方のファイルを持つ者は平文の顧客データを持つに等しく、どちらも持たない者は復元できない。両者を同じ場所に保管することは、保存時暗号化を単なる整理上の慣習に変えてしまう。

退避した鍵は、その鍵で書かれたバックアップを保持する期間以上、保持すること。鍵ローテーションはオンラインかつバッチで再暗号化するため、バックアップがローテーションより前の時点であることは容易に起こる。古い鍵を破棄すれば、そのバックアップは何も異常が起きたように見えないまま読み取れなくなる。

いずれのファイルもコミットしないこと。データ区分に応じた暗号化、アクセス制御、保持ポリシーを適用し、目標復旧水準が要求する場合は物理バックアップ（`pg_basebackup`、WAL アーカイブ）を優先すること。

## リストア

database administrator は、restore role を owner とする隔離targetを作成する。

```sql
CREATE DATABASE merlon_recovery OWNER merlon_migrate TEMPLATE template0;
```

policy上別のdatabase ownerが必要な場合、DBAはfresh databaseの`public` schemaをrestore roleへ明示的に移譲する。schemaに`CREATE`を付与するだけでは不十分である。PostgreSQLではownership移譲時に、移譲先schema ownerがdatabase-level `CREATE`を持つ必要があるため、その一時的なdatabase権限は移譲直後にrevokeする。また、hardening実行者がdatabase ownerまたはsuperuserでない場合、不足するgrantをhardening手順で作成できないため、両roleへdirect database `CONNECT`を事前付与する。

```sql
CREATE DATABASE merlon_recovery OWNER platform_db_owner TEMPLATE template0;
GRANT CONNECT, CREATE ON DATABASE merlon_recovery TO merlon_migrate;
\connect merlon_recovery
ALTER SCHEMA public OWNER TO merlon_migrate;
REVOKE CREATE ON DATABASE merlon_recovery FROM merlon_migrate;
GRANT CONNECT ON DATABASE merlon_recovery TO merlon_app;
```

```bash
export MERLON_MIGRATION_DATABASE_URL='postgres://merlon_migrate:...@host:5432/merlon_recovery'
export MERLON_APP_ROLE='merlon_app' # 任意。これが既定値
make restore BACKUP_FILE=backups/merlon-db-20260726T090000Z.dump

# 本番では追加の明示的な確認が必要:
MERLON_ENV=production make restore \
  BACKUP_FILE=backups/merlon-db-20260726T090000Z.dump \
  RESTORE_FORCE=true
```

この entry point は意図的に in-place restore を行わない。新しい隔離済み target database を作成し、`MERLON_MIGRATION_DATABASE_URL` をそこへ向けること。preflight は、public の relation／routine／type、追加の非system schema、既定外の extension、PostgreSQL large object を拒否する。これらは Merlon migration が作成するすべての object kind を対象とする。したがって、古い archive を新しい Merlon schema の上へ復元して新しい object だけが残ることはなく、非empty target は prompt や変更の前に拒否される。組織定義の collation、conversion、operator、text-search object、publication、subscription、event trigger はこの preflight の対象外であり、隔離 target に事前作成してはならない。

リストア接続には `MERLON_MIGRATION_DATABASE_URL` で指定する target object-owner role を使用する。最小権限の serving role である `merlon_app` は archive object を再作成できず、スクリプトは意図的に `MERLON_DATABASE_URL` へフォールバックしない。promptの前に、スクリプトはこのroleが`public` schemaを管理し、そこで`CREATE`を持つことも検証する。別roleが所有するfresh databaseは、そのownerが上記のとおり`public`を移譲するまで拒否される。prompt は server が報告した接続先 identity と、preflight で fresh target を確認したことを表示する。この entry point は既存 schema を削除しない。

`pg_restore` は `--single-transaction` と `--exit-on-error` で実行するため、archive error で一部だけ復元された object が commit されることはない。失敗した restore では、fresh target に archive object は残らない。API を停止したまま archive または権限を修正し、fresh target に対して再実行すること。

`MERLON_APP_ROLE` で指定する serving role は、事前に存在し、superuser ではなく、対象データベースへの `CREATE` を持たず、`CONNECT` を持つか restore role がそれを付与できる状態でなければならない。スクリプトはこの4条件を確認プロンプトより前に検証する。`audit-hardening.sql` がこれらを強制するのは `pg_restore` の**後**であり、そこで失敗すると、データベースは復元済み・serving-role 権限は未付与・後述の復元後手順は未表示という状態で止まってしまうためである。

`pg_restore` の後、スクリプトは `audit-hardening.sql` の冪等な serving-role 手順を適用する。通常のアプリケーション表には CRUD、監査・ルール有効化の証跡には `SELECT`／`INSERT` のみを付与し、監査 sequence は利用可能にする一方、schema DDL と migration ledger は owner 専用のままにする。archive は意図的に ACL を含まないため、この手順は過去に `--no-privileges` で作成されたバックアップの ACL も再構成する。

自動的に再構成されるのは `MERLON_APP_ROLE` で指定したロールだけである。専用 backup role の既存 object grant／default privilege、および組織固有の auditor、read-only、reporting、integration role の ACL は archive にもこの手順にも含まれない。readiness の確認前に、管理された定義からすべてを別途再適用して検証すること。

確認を求める前に、スクリプトは `psql` で接続し、PostgreSQL が報告する対象ユーザー、サーバーアドレス、ポート、データベース名だけを表示する。libpq は単一の URI 置換ではパスワードを安全に伏せられない形式も受け付けるため、接続文字列そのものは一切表示しない。この接続先を確認してから、続行に `restore` と入力すること。また `MERLON_ENV=production` に対しては `--force` なしで実行を拒否し、Make ターゲットは `RESTORE_FORCE=true` だけをこのフラグへ変換する。よくある致命的な誤りは、誤ったバックアップを復元することではなく、誤ったデータベースへ復元することだからである。

スクリプトはダンプのタイムスタンプに対応するマニフェストと鍵リングのファイルを探す。マニフェストがある場合、そこに記録された `sha256` とダンプを照合し、不一致なら復元を中止する。`pg_restore` は構造的に妥当な custom archive であれば何でも受理するため、途中で切れたコピーや、似た名前が並ぶディレクトリから取り違えたファイルを、このチェックが無ければ何も言わずに復元してしまう。マニフェストが無い場合は警告のみとする。マニフェスト導入前に取得したバックアップや、ダンプ単体で移送されたものも復元可能である必要があるためである。

鍵リングが無い場合も同様に警告する。それが気づくための最後の安価な機会である。復元後はデータベースが正常に見え、顧客属性だけが静かに読めない。

### リストア後の手順

まず隔離された環境にリストアすること。その後、順に次を行う。

1. 対応する鍵リングを `MERLON_ENCRYPTION_KEY_RING` へ読み込む。
2. 対象リリースに必要なマイグレーションを適用する（`make migrate`）。過去のマイグレーションファイルを変更しない。
3. 同じ `MERLON_MIGRATION_DATABASE_URL` と `MERLON_APP_ROLE` で `make audit-harden` を実行する。この必須かつ冪等な2回目の適用により、手順2で作成された表へ権限を付与する。migration ledger があるため、既に復元されたオブジェクトの grant を `make migrate` が再適用することはない。
4. 上記の専用 backup role provisioning（既存 object grant と将来の default privilege を含む）および組織固有の auditor、read-only、reporting、integration role の ACL を管理された定義から再適用し、検証する。
5. API と worker を停止したまま、それらの secret／configuration を更新し、`MERLON_DATABASE_URL` が migration role や backup role ではなく、serving role で fresh target を指すようにする。その後で両processを起動する。
6. readiness を確認する。`GET /healthz/ready` のすべてのチェックが `ok` であること。
7. **暗号化された顧客属性を代表的に読み出す。**
8. `merlon-audit verify` を実行し、検証失敗があれば環境を復旧済みとみなす前に必ず調査する。

手順7が鍵リングの不一致を検出する。他のすべてのチェックを通過しながら、読み取れないデータを生成した復元はありうる。

## ロールバックとはリストアのことである

マイグレーションは前方向のみである。スキーマを変更したリリースのロールバックでは、fresh database を作成し、そこへアップグレード前のbackupをrestoreして検証した後でのみ、`MERLON_DATABASE_URL` をそのfresh targetへ切り替える。このrestore entry pointを現在のdatabaseへ向けてはならない。cutoverによりbackup以降に書き込まれたすべてを失う。[アップグレード](upgrade.md)および[受容リスク](../security/accepted-risks/index.md)を参照。

実務上の帰結として、バックアップ間隔がロールバック時のデータ損失の上限になる。慣習ではなくこの観点で間隔を決めること。

## 復旧証跡

バックアップ識別子、リストア担当者、アプリケーションバージョン、ネイティブエンジン設定ダイジェスト、検証結果、及び例外事項を、組織の変更管理システムに記録する。

初回本番リリース前および以後少なくとも年1回、隔離環境で復元訓練を行う。匿名化した記録には、復元元／先 PostgreSQL 版、リリースコミットとイメージダイジェスト、実施者、開始／完了時刻、RTO 結果、スキーママイグレーション台帳、ヘルスチェック、代表的な暗号化データ読取、`merlon-audit verify` 結果を含める。独立したオブザーバーが記録を承認し、認証情報、暗号鍵、バックアップ保管場所、顧客データは公開リポジトリに置かない。

テストされていないバックアップはロールバック計画ではない。この訓練こそが、本手順書を統制に変えるものである。
