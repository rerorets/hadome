// Package messages holds the wording hadome writes in languages other than English: for each
// language, a catalog from the English format to the same wording in that language. It imports
// nothing of hadome's, so that any package can use it.
//
// Design: docs/design/messages.md
package messages

// Catalog returns the catalog of lang: English formats to lang's formats. English and unknown
// languages have none, so their wording comes out in English.
func Catalog(lang string) map[string]string {
	if lang == "ja" {
		return Japanese
	}
	return nil
}

// Japanese maps each English format to its Japanese format. Both have the same verbs (%s, %d) in
// the same order, so the same arguments fill either. Code, identifiers and configuration keys stay
// as they are.
var Japanese = map[string]string{
	// The summary line of the text output.
	"%d regression(s). %d existing finding(s) unchanged (not blocking).":                   "後退 %d 件。既存の指摘 %d 件は変わっていない（止めない）。",
	"No regressions. %d existing finding(s) unchanged (not blocking).":                     "後退なし。既存の指摘 %d 件は変わっていない（止めない）。",
	"No regressions. %d item(s) need a person's review. %d existing finding(s) unchanged.": "後退なし。人の確認が要るものが %d 件。既存の指摘 %d 件は変わっていない。",

	// Exception markers.
	"exception for %s has no reason":                                        "%s の例外に理由が無い",
	"an exception without a reason cannot be reviewed, so it has no effect": "理由の無い例外は確かめようがないので、効かない",
	"write the reason after 'reason:', or remove the marker":                "'reason:' の後に理由を書くか、印を消す",
	"exception names an unknown rule: %s":                                   "例外が知らないルールを指している: %s",
	"a marker for a rule that does not exist suppresses nothing":            "無いルールの印は何も抑えない",
	"use a rule id that hadome reports, or remove the marker":               "hadome が報告するルールの ID を使うか、印を消す",
	"exception for %s suppresses nothing":                                   "%s の例外が何も抑えていない",
	"stale exceptions pile up and hide what is really excepted":             "古い例外は積もり、本当に例外にしているものを隠す",
	"remove the marker": "印を消す",

	// config/loosened.
	"configuration changes %s: %s → %s":  "設定が %s を変える: %s → %s",
	"configuration removes %s entry: %s": "設定が %s の項目を消す: %s",
	"configuration adds to %s: %s":       "設定が %s に足す: %s",
	"the configuration in this change judges this change; removing, changing or adding to it can make the check more permissive": "この変更を判定するのは、この変更の中の設定である。消す、変える、足すと、検査が緩くなりうる",
	"if the removal is intended, a person approves it; otherwise restore the entry":                                              "意図した変更なら人が認める。そうでなければ項目を元に戻す",

	// tests/no-clock, tests/no-sleep, tests/always-true, tests/no-mock.
	"test reads the clock: %s":                    "テストが時計を読む: %s",
	"a test that reads the current time directly": "今の時刻をじかに読むテスト",
	"the result depends on when the test runs; it passes today and fails at midnight, at month end, or in another time zone": "結果がテストを走らせる時刻で変わる。今日は通っても、真夜中、月末、別のタイムゾーンで落ちる",
	"pass the time in as a value, or inject a clock": "時刻を値として渡すか、時計を注入する",
	"test waits by sleeping: %s":                     "テストが sleep で待つ: %s",
	"a test that waits by sleeping":                  "sleep で待つテスト",
	"a sleep is either too long (a slow suite) or too short (a flaky test); it never waits for the actual event": "sleep は長すぎる（遅いテスト）か短すぎる（不安定なテスト）かのどちらかで、実際の出来事を待つことはない",
	"wait for the event itself: a channel, a callback, or a condition with a deadline that fails loudly":         "出来事そのものを待つ。チャネル、コールバック、または期限つきで、期限を過ぎたらはっきり落ちる条件を使う",
	"assertion cannot fail: %s": "落ちようのない assert: %s",
	"an assertion that cannot fail, such as assertTrue(true) or comparing a value with itself":                               "assertTrue(true) や、値をそれ自身と比べるような、落ちようのない assert",
	"the test reports success no matter what the code does":                                                                  "コードが何をしても、テストは成功を報告する",
	"assert on the behaviour under test":                                                                                     "テストする振る舞いを assert する",
	"test of pure code uses a mock: %s":                                                                                      "純粋なコードのテストがモックを使う: %s",
	"a mock in a test whose targets are all in pure units (units that touch no input or output)":                             "対象がすべて純粋なユニット（入出力に触れないユニット）にあるテストの中のモック",
	"the code under test touches nothing outside; needing a mock means side effects leaked into it, and the mock hides that": "テストするコードは外の何にも触れない。モックが要るなら副作用が入り込んでおり、モックがそれを隠す",
	"move the side effect out of the unit, and test the unit with plain values":                                              "副作用をユニットの外に出し、ユニットは素の値でテストする",

	// tests/skip-without-reason.
	"%s is skipped without a reason":                                                                   "%s が理由なしにスキップされている",
	"a test that is skipped or disabled without a reason":                                              "理由なしにスキップまたは無効にされたテスト",
	"a skipped test protects nothing, and without a reason nobody can decide when it should come back": "スキップしたテストは何も守らない。理由が無いと、いつ戻すべきか誰も決められない",
	"write the reason in the skip itself":                                                              "スキップそのものに理由を書く",
	"write the reason in the skip itself, for example %s":                                              "スキップそのものに理由を書く。例: %s",
	"a comment on or above it.skip: // waits for …":                                                    "it.skip の行かその上のコメント: // waits for …",
	"a reason the language's skip accepts":                                                             "その言語のスキップが受け付ける理由",

	// tests/invariant-unguarded.
	"invariant %s is guarded by no active test":                          "不変条件 %s を守る有効なテストが無い",
	"%s; no symbol matches %s":                                           "%s。%s に合うシンボルが無い",
	"an invariant in configuration that no active test guards":           "設定にあって、有効なテストがどれも守っていない不変条件",
	"the invariant is written down, but nothing fails when it is broken": "不変条件は書いてあるが、破っても何も落ちない",
	"write a test that checks it; built-in types are recognised from the test's calls, others need guards: in the test's doc comment": "確かめるテストを書く。組み込みの種類はテストの呼び出しから見分ける。それ以外はテストの doc コメントに guards: が要る",
	"write a test that checks it and add `guards: %s` to its doc comment":                                                             "確かめるテストを書き、その doc コメントに `guards: %s` を足す",
	"write an active test that calls %s and add `guards: %s` to its doc comment":                                                      "%s を呼ぶ有効なテストを書き、その doc コメントに `guards: %s` を足す",
	"write an active test that calls %s two or more times":                                                                            "%s を2回以上呼ぶ有効なテストを書く",
	"write an active test that calls %s":                                                                                              "%s を呼ぶ有効なテストを書く",
	"write an active test that calls %s with two different users or tenants":                                                          "%s を、異なる2人の利用者または2つのテナントで呼ぶ有効なテストを書く",
	"%s and %s": "%s と %s",

	// tests/guards-nothing, tests/no-assertion, tests/duplicate.
	"%s calls no code of this repository and reads none of its values":                        "%s はこのリポジトリのコードを呼ばず、その値も読まない",
	"a new test that calls no code of the repository and reads none of its values":            "リポジトリのコードを呼ばず、その値も読まない新しいテスト",
	"the code can change in any way without this test noticing":                               "コードがどう変わっても、このテストは気づかない",
	"call the code the test means to check, or delete the test if it is not needed":           "テストで確かめたいコードを呼ぶ。要らないテストなら消す",
	"%s checks nothing: it has no assertion":                                                  "%s は何も確かめていない。assert が無い",
	"a new test with no assertion, in its body or in the helpers of its test files":           "本体にも、テストのファイルの補助関数にも assert が無い新しいテスト",
	"the test passes whatever the code returns, and fails only if something panics or throws": "コードが何を返してもテストは通り、panic か例外のときだけ落ちる",
	"assert on the behaviour the test means to check; if the function is an assertion hadome does not know, add it to rules.tests.assertions; if the test only checks that nothing throws, add an exception with the reason": "テストで確かめたい振る舞いを assert する。hadome の知らない assert の関数なら rules.tests.assertions に足す。例外が起きないことだけを確かめるテストなら、理由つきの例外を書く",
	"%d tests have the same body: %s": "%d 本のテストの本体が同じ: %s",
	"tests with the same body, ignoring names, comments and whitespace; a new copy is a regression":             "名前、コメント、空白を除いて本体が同じテスト。新しい写しは後退になる",
	"checking the same thing twice guards nothing more, and a change of behaviour has to be made in every copy": "同じことを2回確かめても守るものは増えない。振る舞いを変えるときは、写しのすべてを直すことになる",
	"delete all but one, or change the input if they were meant to check different cases":                       "1本だけ残して消す。別の場合を確かめるつもりだったなら、入力を変える",

	// tests/purity-lost.
	"%s is no longer pure: it now reaches input or output through its imports":                                             "%s はもう純粋でない。import を通して入出力に届くようになった",
	"a unit that was pure in base and reaches input or output in head, through its imports":                                "base で純粋だったのに、head では import を通して入出力に届くユニット",
	"side effects entered code that used to take values and return values; sometimes intended, often a design slip":        "値を受けて値を返していたコードに副作用が入った。意図したこともあるが、設計の誤りのことが多い",
	"if intended, a person approves it; otherwise move the side effect to a unit that is not pure and pass its results in": "意図したなら人が認める。そうでなければ副作用を純粋でないユニットに移し、その結果を渡す",

	// tests/scope-shrunk, tests/target-unknown.
	"guarded scope shrank: %s is no longer exercised by any active test (was: %s)":                                       "守る範囲が狭まった: %s を呼ぶ有効なテストが無くなった（前は %s）",
	"the behaviour of %s can now change without any test noticing":                                                       "%s の振る舞いが変わっても、どのテストも気づかなくなった",
	"keep or restore a test that calls %s, or remove %s itself if it is no longer needed":                                "%s を呼ぶテストを残すか戻す。要らなくなったなら %s そのものを消す",
	"code that an active test called in base and no active test calls in head, while the code is still there":            "base では有効なテストが呼んでいたのに、head ではどの有効なテストも呼ばず、コードは残っているもの",
	"the behaviour of that code can now change without any test noticing":                                                "そのコードの振る舞いが変わっても、どのテストも気づかなくなった",
	"keep or restore a test that calls it, or remove the code itself if it is no longer needed":                          "それを呼ぶテストを残すか戻す。要らなくなったならコードそのものを消す",
	"could not identify what %s tests":                                                                                   "%s が何をテストしているか特定できなかった",
	"a new test none of whose calls hadome can identify":                                                                 "呼び出しをどれも hadome が特定できない新しいテスト",
	"hadome cannot tell whether removing this test would shrink what the suite guards":                                   "このテストを消したとき、テスト全体が守る範囲が狭まるかどうか、hadome には分からない",
	"call the code under test through its package-level API (a function, or a method on a value built by a constructor)": "テストするコードを、パッケージの API（関数、またはコンストラクタで作った値のメソッド）から呼ぶ",

	// size/.
	"%s: %s %d, limit %d": "%s: %s %d（上限 %d）",
	"%s; grew from %d":    "%s。base の %d から増えた",
	"product code over the %s limit (%s); an existing one that grows is a regression too":                                "%s の上限（%s）を超えた製品のコード。既にあるものが大きくなるのも後退になる",
	"past the limit, the code no longer fits in one reading, and changes to it are made without understanding all of it": "上限を超えると、コードは一度に読み切れなくなり、全体を分からないまま変更されるようになる",
	"%s; raising the limit (rules.size.%s) is a change a person reviews":                                                 "%s。上限（rules.size.%s）を上げるのは、人が確かめる変更である",
	"lines":                 "行数",
	"parameters":            "引数の数",
	"cyclomatic complexity": "循環的複雑度",
	"methods":               "メソッドの数",
	"split the file along a line it already has: one file per type or per concern":      "ファイルを、既にある切れ目で分ける。型ごと、または関心ごとに1つのファイルにする",
	"extract the steps of the function into functions of their own":                     "関数の手順を、それぞれ別の関数に切り出す",
	"group parameters that travel together into a type":                                 "いつも一緒に渡す引数を1つの型にまとめる",
	"move each branch's work into a function, or replace a chain of cases with a table": "分岐ごとの処理を関数に移すか、case の連なりを表に置き換える",
	"split the interface by who uses it; each caller should need only what it calls":    "インターフェースを使う側ごとに分ける。呼ぶ側が要るのは、自分が呼ぶものだけにする",

	// comments/.
	"TODO without the required form: %s":                                                                                               "決まった形になっていない TODO: %s",
	"a new TODO, FIXME or XXX comment that does not match the configured form":                                                         "設定した形に合わない、新しい TODO、FIXME、XXX のコメント",
	"a note to do something later that does not say when is never done; it stays as noise and misleads readers about what is finished": "いつやるかを書いていない「後でやる」は、やられない。雑音として残り、何が終わったかを読み手に取り違えさせる",
	"write it in the configured form (rules.comments.todo), do it now, or delete it":                                                   "設定した形（rules.comments.todo）で書くか、今やるか、消す",
	"write it in the form %s (for example %s), do it now, or delete it":                                                                "%s の形（例: %s）で書くか、今やるか、消す",
	"write it in the form %s, do it now, or delete it":                                                                                 "%s の形で書くか、今やるか、消す",
	"exported %s %s has no doc comment":                                                                                                "公開した %s %s に doc コメントが無い",
	"value %s has no description":                                                                                                      "値 %s に description が無い",
	"a new exported function, method or type of product code without a doc comment, or a new Terraform variable or output or Ansible role default without a description":                                   "doc コメントの無い、製品のコードの新しい公開した関数、メソッド、型。description の無い、新しい Terraform の variable と output、Ansible のロールの defaults の変数",
	"whoever sets or reads the value reads its description instead of the code that uses it; without one, each of them reads that code and guesses what the value is for":                                  "値を渡す側も読む側も、値を使うコードではなく description を読む。無ければ、みなそのコードを読んで、何のための値かを推し量ることになる",
	"add a description that says what the value is for and what a caller would otherwise get wrong: Terraform's description argument, or the variable's description in the role's meta/argument_specs.yml": "何のための値か、書かなければ使う側が取り違えることを description に書く。Terraform は description の引数、Ansible はロールの meta/argument_specs.yml のその変数の description に書く",
	"callers read the doc comment instead of the body; without it, every caller reads the implementation and guesses":                                                                                      "呼ぶ側は本体ではなく doc コメントを読む。無ければ、呼ぶ側はみな実装を読んで推し量ることになる",
	"write one sentence that starts with the name and says what it does or is, and what a caller would otherwise get wrong":                                                                                "名前で始まる1文で、何をするか、何であるか、書かなければ呼ぶ側が取り違えることを書く",
	"comment refers to %s, which does not exist":                                                                 "コメントが指す %s が無い",
	"a comment that names a Markdown file the version does not have":                                             "その版に無い Markdown のファイルを指すコメント",
	"a pointer to a document promises that the reasoning is written down; a broken one sends the reader nowhere": "文書を指すことは、理由が書き残してあるという約束である。指す先が無ければ、読み手はどこにもたどり着けない",
	"point at the document's current path, or remove the reference":                                              "文書の今のパスを指すか、参照を消す",

	// deps/.
	"import not in the diagram: %s → %s (%s)": "図に無い import: %s → %s（%s）",
	"no node covers the importing unit":       "import する側のユニットを覆うノードが無い",
	"no node covers the imported unit":        "import される側のユニットを覆うノードが無い",
	"no arrow %s --> %s":                      "矢印 %s --> %s が無い",
	"an import between the repository's own units that the dependency diagram does not draw":                                                                   "リポジトリ自身のユニットの間の、依存の図に描かれていない import",
	"the diagram is how people and agents know what may depend on what; an import it does not draw makes it wrong":                                             "人とエージェントは、何が何に依存してよいかを図で知る。図に描かれていない import は、図を誤りにする",
	"the diagram (%s) is how people and agents know what may depend on what; an import it does not draw makes it wrong":                                        "人とエージェントは、何が何に依存してよいかを図（%s）で知る。図に描かれていない import は、図を誤りにする",
	"depend on what the diagram allows, often through an interface the importing unit declares; if the design changed, draw the arrow, which a person reviews": "図が許すものに依存する。import する側のユニットが宣言するインターフェースを通すことが多い。設計が変わったなら矢印を描く。矢印は人が確かめる",
	"the diagram's node %s names %s, which holds no unit":                                                                                                      "図のノード %s が指す %s に、ユニットが無い",
	"a node of the dependency diagram whose label names no unit of the version, or a planned node whose directory already holds one":                           "依存の図のノードのうち、ラベルがその版のどのユニットも指さないもの。または、ディレクトリにもうユニットがある予定の箱",
	"a box that names nothing makes the diagram look complete when it is not":                                                                                  "何も指さない箱があると、図が完全でないのに完全に見える",
	"correct the label to the unit's directory, remove the node, or mark a box not created yet with the class planned":                                         "ラベルをユニットのディレクトリに直すか、ノードを消す。まだ作っていない箱なら、クラス planned を付ける",
	"Invariant candidates this change adds (information only; propose them to a person, `hadome suggest` prints them as configuration):":                       "この変更で増えた不変条件の候補（知らせるだけ。人に提案する。`hadome suggest` が設定の形で出す）:",
	"Settings this change introduces with .hadome.yml (information only; base has no configuration, so nothing counts as loosening):":                          "この変更が .hadome.yml で入れる設定（知らせるだけ。base に設定が無いので、緩めることには数えない）:",
	"the diagram's node %s is planned, but %s already holds a unit":                                                                                            "図のノード %s は予定の箱だが、%s にはもうユニットがある",
	"a planned box whose package exists tells readers it is still to be written":                                                                               "パッケージがあるのに予定の箱のままだと、まだ書かれていないと読み手に伝わってしまう",
	"remove the class planned from node %s": "ノード %s からクラス planned を外す",

	// unknown/.
	"hadome could not read %s": "hadome は %s を読めなかった",
	"it does not parse, or this build of hadome has no reader for %s; no rule saw what it does":               "構文として読めないか、この hadome のビルドに %s を読む部品が無い。どのルールも、そのファイルが何をするかを見ていない",
	"make it parse, or use a build of hadome that reads %s (every prebuilt binary does)":                      "構文として読めるように直すか、%s を読める hadome のビルドを使う（配布しているバイナリはどれも読める）",
	"a new file of a supported language that hadome could not read":                                           "対応している言語なのに、hadome が読めなかった新しいファイル",
	"no rule saw what the file does, so nothing it adds was checked":                                          "どのルールもそのファイルが何をするかを見ていないので、そのファイルが足したものは何も検査されていない",
	"make it parse, or use a build of hadome that reads its language (every prebuilt binary does)":            "構文として読めるように直すか、その言語を読める hadome のビルドを使う（配布しているバイナリはどれも読める）",
	"the destination of this import is decided only at run time: %s":                                          "この import の行き先は実行するまで決まらない: %s",
	"a new import whose destination is decided only at run time, such as import(name) or Class.forName(name)": "import(name) や Class.forName(name) のように、行き先が実行するまで決まらない新しい import",
	"no rule can tell what it reaches, so dependencies and purity are judged without it":                      "どこに届くかをどのルールも見分けられないので、依存と純粋さはそれを抜きに判定される",
	"import the destination by name; if it must stay dynamic, a person approves it":                           "行き先を名前で import する。動的なままにするしかないなら、人が認める",

	// mutation/survived.
	"%s: changing `%s` to `%s` does not fail any test":                                                                                          "%s: `%s` を `%s` に変えても、どのテストも落ちない",
	"%s: removing `%s` does not fail any test":                                                                                                  "%s: `%s` を消しても、どのテストも落ちない",
	"a break of a function the change touched that no test notices":                                                                             "変更が触れた関数を壊しても、どのテストも気づかないもの",
	"the code can do something else than it does now, and the tests still pass":                                                                 "コードが今と違うことをしても、テストは通る",
	"add or strengthen a test that checks what the broken line decides; if the break cannot change behaviour, add an exception with the reason": "壊した行が決めていることを確かめるテストを足すか、強める。壊しても振る舞いが変わりえないなら、理由つきの例外を書く",
	"hadome cannot break %s code yet, so the functions this change touched in %s were not tried":                                                "hadome はまだ %s のコードを壊せないので、この変更が触れた %s の関数は試していない",
	"untried code is not known to be guarded":                                                                                                   "試していないコードは、守られているかどうか分からない",
	"a person checks the tests of those functions":                                                                                              "それらの関数のテストを人が確かめる",
	"%d mutants beyond mutation.max-mutants were not tried, in %s":                                                                              "mutation.max-mutants を超えた %d 個の変異を試していない。対象: %s",
	"split the change, or have a person decide whether to raise mutation.max-mutants":                                                           "変更を分けるか、mutation.max-mutants を上げるかどうかを人が決める",

	// Product-specific gates of kind measure.
	"%s: %s is no longer guarded (was: %s)":                                               "%s: %s が守られなくなった（前は %s）",
	"the product's own gate %s reported it in the base version":                           "製品自身の門番 %s が、base の版でこれを報告していた",
	"restore it, or, if it was removed on purpose, make the gate answer it as legitimate": "元に戻す。意図して消したなら、門番がそれを正当な削除と答えるようにする",
}
