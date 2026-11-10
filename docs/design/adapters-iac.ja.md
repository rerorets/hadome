# Terraform と Ansible のアダプタ

この文書は [adapters.ja.md](adapters.ja.md) のうち Terraform と Ansible の部分を書きます。

- どちらもインフラの構成を書く言語で、関数もクラスもありません
- そのため、ほかの言語と同じ事実のうち、意味の通るものだけを埋めます
- 埋めない事実はルールからは空に見えます。そのルールは何も報告しません

## 形

```mermaid
classDiagram
  class Language {
    <<interface>>
    +Name() string
    +Claims(path string) bool
    +Analyze(r *reuse.Run, c config.Analysis, paths []string) (*facts.Analysis, error)
  }
  class terraform {
    <<package>>
  }
  class ansible {
    <<package>>
  }
  class hclsyntax {
    <<external>>
    github.com/hashicorp/hcl/v2
  }
  class goyaml {
    <<external>>
    github.com/goccy/go-yaml
  }
  terraform ..|> Language
  ansible ..|> Language
  terraform --> hclsyntax : 読む
  ansible --> goyaml : 読む
```

- **パッケージ。** `internal/adapter/terraform` と `internal/adapter/ansible` を足します。どちらも Go のアダプタと同じく純粋な Go で書くので、cgo の無いビルドでも読めます
- **Terraform のパーサー。** Terraform 自身が使う HCL のパーサー（`hashicorp/hcl/v2` の `hclsyntax`）で読みます。tree-sitter の HCL の文法は使いません。Go のバインディングを持つバージョンが ABI 15 で、hadome が使う Go のバインディング（ABI 14 まで）では読めないからです
- **ライセンス。** `hashicorp/hcl/v2` のライセンスは MPL-2.0 です。MPL はファイル単位で効くので、hadome のコードは MIT のままでかまいません。配る実行ファイルには MPL-2.0 の本文と、`hashicorp/hcl/v2` のソースの在りかを書いた文書を添えます
- **Ansible のパーサー。** 設定を読むのに使っている `goccy/go-yaml` で読みます。依存は増えません
- **依存の図。** `internal/adapter --> internal/adapter/terraform & internal/adapter/ansible` の矢印と、2つから `engine`、`config`、`facts` への矢印を足します
- **`ClaimsConfigured`。** `Claims(path)` はパスしか受け取らないので、設定の `files.ansible` を見られません。そこで、Ansible のアダプタは `ClaimsConfigured(c, path)` も持ちます。`internal/adapter` は、これを持つ言語には `ClaimsConfigured` で受け持つかを尋ねます

## ファイルと役割

| 言語 | 受け持つファイル | テストのファイル | 役割が other のもの |
|---|---|---|---|
| Terraform | `*.tf` | `*.tftest.hcl` | `*.tfvars`（値だけで、読むものが無い） |
| Ansible | `roles/<ロール>/` の下の `tasks/`、`handlers/`、`defaults/`、`vars/`、`meta/` の `*.yml` と `*.yaml`。`playbooks/` の下と、ルートの `site.yml`、`playbook.yml` | `molecule/<シナリオ>/` の下の `*.yml`（ロールの下にあるものも含む） | なし |

- `roles/`、`playbooks/`、`molecule/` は、どの深さにあってもかまいません。大きなリポジトリの中の Ansible（`ansible/roles/web`）も読むためです
- `site.yml` と `playbook.yml` はルートのものだけを受け持ちます
- `files.tests` は Terraform と Ansible のファイルには当てません。テストかどうかは Go と同じく言語の側で決まるからです。Terraform では `*.tftest.hcl` という名前で、Ansible では Molecule のシナリオの配置で決まります
- Ansible のアダプタは YAML のファイルをすべては受け持ちません。YAML は CI の設定や `.hadome.yml` のように、Ansible でないものにも使われるからです。受け持つかどうかはほかの言語と同じくパスだけで決めます

この配置から外れたプレイブック（ルートの `deploy.yml` など）は、`.hadome.yml` の `files.ansible` に glob で足します。

```yaml
files:
  ansible: ["deploy/*.yml", "ops/site-*.yml"]
```

- 足したファイルはプレイブックとして読みます
- 項目を消すと、読むファイルが減るので緩める変更になります（`LooserWhenRemoved`）。足すのは緩めません

## ユニット

| 言語 | ユニット | ID | 場所 | シンボルの ID |
|---|---|---|---|---|
| Terraform | モジュール（ディレクトリ） | そのディレクトリ | そのディレクトリ | `<directory>.var.<name>`、`<directory>.output.<name>` |
| Ansible | ロール | ロールの名前 | `roles/<ロール>`。プレイブックはそのディレクトリ | `<role>.<name>` |

Terraform のシンボルの ID にはモジュールの中での呼び方（`var.region`、`output.region`）を入れます。同じ名前の `variable` と `output` を区別するためです。ルートのモジュールでは前にディレクトリを付けません（`var.region`、`output.vpc_id`）。

## import

| 言語 | import になるもの | 内部になるとき | 外部の名前 |
|---|---|---|---|
| Terraform | `module` ブロックの `source`。`terraform { required_providers { … } }` の各 `source` | `source` が `./` か `../` で始まる。そのディレクトリに解決する | レジストリのアドレス（`hashicorp/consul/aws`）、プロバイダのアドレス（`hashicorp/aws`）。git や `s3::` などの `source` は、`?ref=` などのクエリと `//` のあとのサブディレクトリを除いたもの |
| Ansible | `meta/main.yml` の `dependencies`、プレイの `roles:`、`include_role` と `import_role`（`ansible.builtin.` を付けたものを含む）の `name` | リポジトリにそのロールがある | ロールかコレクションの名前（`community.general`） |

**行き先が決まらない import**

- Ansible で、ロールの名前に `{{ }}` を使ったものは、実行するまで行き先が決まりません。そうした import（`Import.Unknown`）は `unknown/import` に回します
- Terraform の `source` には式を書けないので、行き先が決まらない import はありません

**入出力になる import**

- Ansible の外部のロールとコレクションの import が入出力になるのは、`rules.tests.io` に挙げたときだけです
- Terraform の `required_providers` の import は、入出力（`IO`）とします。モジュールはプロバイダを通して外に触れるからです
- Terraform の `module` の import が入出力になるのは、`rules.tests.io` に挙げたときだけです
- `resource` か `data` のブロックがあるファイルは、そのプロバイダの import を持つものとし、入出力とします。`required_providers` に書いていなくても、プロバイダに触れるからです
- そのプロバイダは `provider` の引数か、型の名前の最初の語（`aws_vpc` なら `aws`）を、同じモジュールの `required_providers` で引いて決めます。そこに無ければ、Terraform と同じく `hashicorp/<名前>` とします
- `variable`、`output`、`locals` だけのモジュールは外に触れないので純粋なままです

## シンボル

Terraform の `variable` と `output` はモジュールの入口と出口です。そこで、どちらも種類 `value` のシンボルとして公開にします。`description` があれば、doc があるとみなします。これで、`comments/missing-doc` が `description` の無い新しい `variable` と `output` を報告します。

| 言語 | シンボル | 公開 | doc にあたるもの |
|---|---|---|---|
| Terraform | `variable` と `output`（種類 `value`） | いつも | `description` の属性 |
| Ansible | ロールの `defaults/main.yml` の各変数（種類 `value`） | 名前が `_` で始まらない | `meta/argument_specs.yml` の、その変数の `description` |

- `facts` に種類 `SymbolValue`（`"value"`）を足します。関数を数えるルールは種類で絞っているので、`value` は数えません。例: `size/` の複雑度と引数、`hadome mutate` が壊す関数
- `resource`、`data`、`locals`、Ansible のタスクはシンボルにしません。外から名指されるものではないからです

## テスト

| 言語 | テストとは | assert とみなすもの | 呼ぶもの（`Calls`） | 危険 |
|---|---|---|---|---|
| Terraform | `*.tftest.hcl` の各 `run` ブロック。名前は `run` のラベル | `assert` ブロック、`expect_failures` | `variables` で値を渡す `variable`、`assert` の `condition` が読む `output`。対象は、`run` の中の `module { source }` があればそのモジュール、無ければテストのファイルのモジュール（`tests/` の下なら1つ上のディレクトリ） | `mock_provider`、`override_resource`、`override_data`、`override_module` を `mock` とする |
| Ansible | Molecule の各シナリオの `verify.yml`。名前はシナリオの名前 | `assert`（`ansible.builtin.assert`）のタスク、`failed_when` を付けたタスク | 同じシナリオの `converge.yml` が使う、リポジトリのロール | `pause`（`ansible.builtin.pause`）を `sleep` とする |

- どちらにも、スキップの書き方はありません
- Ansible のテストが呼ぶものはロール、つまりユニットそのものです。そこで、`Call.Target` にはロールの名前（ユニットの ID）を入れます。ロールが head に残っていれば、`tests/scope-shrunk` はそのロールを消されたものとみなしません
- `converge.yml` が `{{ }}` で名指すロールは、特定できなかった呼び出し（`Unresolved`）として数えます
- ただし、`roles/<ロール>/molecule/<シナリオ>/` のシナリオが `{{ lookup('env', 'MOLECULE_PROJECT_DIRECTORY') | basename }}` で名指すロールは、そのシナリオを含むロールとします
  - Molecule はこの環境変数にロールのディレクトリを入れるので、推測ではありません
  - 空白の違い、引用符の種類、`ansible.builtin.env` の書き方は問いません
  - ロールの外のシナリオではほかの `{{ }}` と同じく特定できません
- Terraform で `run` の外に書いた `variables` や `mock_provider` などは、そのファイルのすべての `run` に効くものとして読みます
- テストの本体の指紋（`Test.Body`）はブロックかタスクの字句から、ほかの言語と同じ決まりで作ります

## コメント

| 言語 | コメント | ディレクティブ |
|---|---|---|
| Terraform | `#`、`//`、`/* */` | `tflint-ignore`、`checkov:skip`、`trivy:ignore` |
| Ansible | `#` | `noqa`、`yamllint` |

例外の印（`gate:allow`）はどちらの言語でもコメントに書きます。

## 効くルール

| ルール | Terraform | Ansible |
|---|---|---|
| `deps/` | モジュールの依存。プロバイダを使ってよいモジュールを絞れる | ロールの依存 |
| `comments/` | `description` の無い `variable` と `output` | `argument_specs` に説明の無い変数 |
| `size/` | ファイルの行数 | ファイルの行数 |
| `tests/` | `run` の数と assert。`mock_provider` と `override_*` はモックとして `tests/no-mock` が見る | assert の無い `verify.yml`。唯一のシナリオを失ったロール（`tests/scope-shrunk`）。リポジトリのロールを使わないシナリオ（`tests/guards-nothing`）。`pause` |
| `unknown/` | 読めなかったファイル | 読めなかったファイル。行き先の決まらないロール |
| `hadome mutate` | 関数が無いので壊さない | 同じ |

## 読めなかったファイル

次のファイルには `Unread` の印を付けます。Go のアダプタと同じく、そのファイルからは何も読みません。

- HCL のパーサーが誤りを返した `*.tf`
- YAML として読めなかったファイル

## 不変条件

1. どちらのアダプタもコードを実行せず、`terraform` も `ansible` も呼びません。読むのは、ファイルの字句と構文だけです
2. 受け持つファイルは読むか、読めなかったと報告するかのどちらかです。無視されることはありません
3. 受け持つかどうかはパスと `files.ansible` だけで決まります。ファイルの中身は見ません

## 意図して入れなかったもの

- **Terraform:** `*.tf.json`、Terragrunt、`terraform` の backend と workspace
- **Ansible:** インベントリ、コレクションのプラグイン（Python のファイルは Python のアダプタが読む）、`include_tasks` のファイル名での読み込み
- **Ansible のテスト:** Molecule 以外のもの（`ansible-test` など）
- **式の評価:** `count`、`for_each`、Jinja の式の値は読みません
