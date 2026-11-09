package sql

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// viewpoint: behaviour
// target: sql.splitter.split
// Splitting blanks what only the client reads and cuts at the separators, keeping every offset and
// line: SQL*Plus commands only where a statement may start (a blank line changes nothing), SET
// TRANSACTION kept as SQL; MySQL's DELIMITER and the statements it ends, each delimiter turned into
// ; and blanks; GO between T-SQL batches; the undo part of a dbmate migration. A piece of blanks
// only is dropped. In the expected text, · is a blank.
func TestSplitBlanksClientFormsAndCutsAtSeparators(t *testing.T) {
	cases := []struct {
		name string
		d    tableDialect
		src  string
		want []string
	}{
		{"SQL*Plus", oracleDialect,
			"SET ECHO OFF\nUPDATE t\n\nSET a = 1;\nSET SERVEROUTPUT ON\nSET TRANSACTION READ ONLY;\n/\nSELECT 1 FROM dual;\n",
			[]string{
				"0@1:············\nUPDATE t\n\nSET a = 1;\n···················\nSET TRANSACTION READ ONLY;\n",
				"81@7:·\nSELECT 1 FROM dual;\n",
			}},
		{"DELIMITER", mysqlDialect, "DELIMITER //\nSELECT 1 //SELECT 2 //\nDELIMITER ;\nSELECT 3;\n",
			[]string{"0@1:············\nSELECT 1 ;·SELECT 2 ;·\n···········\nSELECT 3;\n"}},
		{"GO", tsqlDialect, "SELECT 1\nGO\n:setvar x 1\nSELECT 2\nGO 2\n",
			[]string{"0@1:SELECT 1\n", "9@2:··\n···········\nSELECT 2\n"}},
		{"dbmate", postgresqlDialect,
			"-- migrate:up\nCREATE TABLE t (id int);\n-- migrate:down\nDROP TABLE t;\n-- migrate:up\nCREATE TABLE u (id int);\n",
			[]string{"0@1:-- migrate:up\nCREATE TABLE t (id int);\n···············\n·············\n-- migrate:up\nCREATE TABLE u (id int);\n"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, s := range tc.d.Split([]byte(tc.src)) {
				got = append(got, fmt.Sprintf("%d@%d:%s", s.Start, s.Line, strings.ReplaceAll(s.Text, " ", "·")))
			}
			var want []string
			for _, w := range tc.want {
				want = append(want, strings.ReplaceAll(w, " ", "·"))
			}
			if !slices.Equal(got, want) {
				t.Errorf("pieces\n%q\nwant\n%q", got, want)
			}
		})
	}
}
