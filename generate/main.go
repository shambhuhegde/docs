package main

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/cockroachdb/docs/generate/extract"
	"github.com/spf13/cobra"
)

func main() {
	var (
		inputPath  string
		outputPath string
	)

	read := func() io.Reader {
		var r io.Reader = os.Stdin
		if inputPath != "" {
			f, err := os.Open(inputPath)
			if err != nil {
				log.Fatal(err)
			}
			defer f.Close()
			b, err := ioutil.ReadAll(f)
			if err != nil {
				log.Fatal(err)
			}
			r = bytes.NewReader(b)
		}
		return r
	}

	write := func(b []byte) {
		var w io.Writer = os.Stdout
		if outputPath != "" {
			f, err := os.Create(outputPath)
			if err != nil {
				log.Fatal(err)
			}
			defer f.Close()
			w = f
		}
		if _, err := w.Write(b); err != nil {
			log.Fatal(err)
		}
	}

	var addr string

	cmdBNF := &cobra.Command{
		Use:   "bnf",
		Short: "Write EBNF to stdout from sql.y",
		Run: func(cmd *cobra.Command, args []string) {
			b, err := runBNF(addr)
			if err != nil {
				log.Fatal(err)
			}
			write(b)
		},
	}

	cmdBNF.Flags().StringVar(&addr, "addr", "https://raw.githubusercontent.com/cockroachdb/cockroach/master/sql/parser/sql.y", "Location of sql.y file. Can also specify a local file.")

	var (
		topStmt string
		descend bool
		inline  []string
	)

	cmdParse := &cobra.Command{
		Use:   "reduce",
		Short: "Reduces and simplify an EBNF file to a smaller grammar",
		Long:  "Reads from stdin, writes to stdout.",
		Run: func(cmd *cobra.Command, args []string) {
			b, err := runParse(read(), inline, topStmt, descend, nil, nil)
			if err != nil {
				log.Fatal(err)
			}
			write(b)
		},
	}

	cmdParse.Flags().StringVar(&topStmt, "stmt", "stmt_block", "Name of top-level statement.")
	cmdParse.Flags().BoolVar(&descend, "descend", true, "Descend past -stmt.")
	cmdParse.Flags().StringSliceVar(&inline, "inline", nil, "List of statements to inline.")

	cmdRR := &cobra.Command{
		Use:   "rr",
		Short: "Generate railroad diagram from stdin, writes to stdout",
		Run: func(cmd *cobra.Command, args []string) {
			b, err := runRR(read())
			if err != nil {
				log.Fatal(err)
			}
			write(b)
		},
	}

	cmdBody := &cobra.Command{
		Use:   "body",
		Short: "Extract HTML <body> contents from stdin, writes to stdout",
		Run: func(cmd *cobra.Command, args []string) {
			s, err := extract.InnerTag(read(), "body")
			if err != nil {
				log.Fatal(err)
			}
			write([]byte(s))
		},
	}

	cmdFuncs := &cobra.Command{
		Use:   "funcs",
		Short: "Generates functions.md and operators.md",
		Run: func(cmd *cobra.Command, args []string) {
			generateFuncs()
		},
	}

	var (
		baseDir string
	)

	rootCmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate SVG diagrams from SQL grammar",
		Long:  `With no arguments, generates SQL diagrams for all statements.`,
		Run: func(cmd *cobra.Command, args []string) {
			bnf, err := runBNF(addr)
			if err != nil {
				log.Fatal(err)
			}
			br := func() io.Reader {
				return bytes.NewReader(bnf)
			}
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				g, err := runParse(br(), nil, "stmt_block", true, nil, nil)
				if err != nil {
					log.Fatal(err)
				}
				rr, err := runRR(bytes.NewReader(g))
				if err != nil {
					log.Fatal(err)
				}
				body, err := extract.InnerTag(bytes.NewReader(rr), "body")
				body = strings.SplitN(body, "<hr/>", 2)[0]
				body += `<p>generated by <a href="http://www.bottlecaps.de/rr/ui">Railroad Diagram Generator</a></p>`
				body = fmt.Sprintf("<div>%s</div>", body)
				if err != nil {
					log.Fatal(err)
				}
				if err := ioutil.WriteFile(filepath.Join(baseDir, "grammar.html"), []byte(body), 0644); err != nil {
					log.Fatal(err)
				}
			}()

			specs := []stmtSpec{
				// TODO(mjibson): improve SET filtering
				// TODO(mjibson): improve SELECT display
				{name: "alter_table_stmt", inline: []string{"alter_table_cmds", "alter_table_cmd", "column_def", "opt_drop_behavior", "alter_column_default", "opt_column", "opt_set_data"}},
				{name: "begin_transaction", stmt: "transaction_stmt", inline: []string{"opt_transaction", "opt_transaction_mode_list", "transaction_iso_level", "transaction_user_priority"}, match: regexp.MustCompile("'BEGIN'")},
				{name: "commit_transaction", stmt: "transaction_stmt", inline: []string{"opt_transaction"}, match: regexp.MustCompile("'COMMIT'")},
				{name: "create_database_stmt"},
				{name: "create_index_stmt", inline: []string{"opt_unique", "opt_name", "index_params"}},
				{name: "create_table_stmt", inline: []string{"opt_table_elem_list", "table_elem_list", "table_elem"}},
				{name: "delete_stmt", inline: []string{"relation_expr_opt_alias", "where_clause", "returning_clause", "target_list", "target_elem"}},
				{name: "drop_database", stmt: "drop_stmt", match: regexp.MustCompile("'DROP' 'DATABASE'")},
				{name: "drop_index", stmt: "drop_stmt", match: regexp.MustCompile("'DROP' 'INDEX'"), inline: []string{"opt_drop_behavior"}},
				{name: "drop_stmt", inline: []string{"any_name_list", "any_name", "qualified_name_list", "qualified_name"}},
				{name: "drop_table", stmt: "drop_stmt", match: regexp.MustCompile("'DROP' 'TABLE'")},
				{name: "explain_stmt", inline: []string{"explainable_stmt", "explain_option_list"}},
				{name: "grant_stmt", inline: []string{"privileges", "privilege_list", "privilege", "privilege_target", "grantee_list"}},
				{name: "insert_stmt", inline: []string{"insert_target", "insert_rest", "returning_clause"}},
				{name: "release_savepoint", stmt: "release_stmt", inline: []string{"savepoint_name"}},
				{name: "rename_column", stmt: "rename_stmt", match: regexp.MustCompile("'ALTER' 'TABLE' .* 'RENAME' opt_column")},
				{name: "rename_database", stmt: "rename_stmt", match: regexp.MustCompile("'ALTER' 'DATABASE'")},
				{name: "rename_index", stmt: "rename_stmt", match: regexp.MustCompile("'ALTER' 'INDEX'")},
				{name: "rename_table", stmt: "rename_stmt", match: regexp.MustCompile("'ALTER' 'TABLE' .* 'RENAME' 'TO'")},
				{name: "revoke_stmt", inline: []string{"privileges", "privilege_list", "privilege", "privilege_target", "grantee_list"}},
				{name: "rollback_transaction", stmt: "transaction_stmt", inline: []string{"opt_transaction"}, match: regexp.MustCompile("'ROLLBACK'")},
				{name: "savepoint_stmt", inline: []string{"savepoint_name"}},
				{name: "select_stmt", inline: []string{"select_no_parens", "simple_select", "opt_sort_clause", "select_limit"}},
				{name: "set_stmt", inline: []string{"set_rest", "set_rest_more", "generic_set"}, exclude: regexp.MustCompile("CHARACTERISTICS"), replace: map[string]string{"'TRANSACTION' transaction_mode_list | ": ""}},
				{name: "set_transaction", stmt: "set_stmt", inline: []string{"set_rest", "transaction_mode_list", "transaction_iso_level", "transaction_user_priority"}, replace: map[string]string{" | set_rest_more": ""}, match: regexp.MustCompile("'TRANSACTION'")},
				{name: "show_columns", stmt: "show_stmt", match: regexp.MustCompile("'SHOW' 'COLUMNS'")},
				{name: "show_databases", stmt: "show_stmt", match: regexp.MustCompile("'SHOW' 'DATABASES'")},
				{name: "show_grants", stmt: "show_stmt", inline: []string{"on_privilege_target_clause", "privilege_target", "for_grantee_clause", "grantee_list"}, match: regexp.MustCompile("'SHOW' 'GRANTS'")},
				{name: "show_index", stmt: "show_stmt", match: regexp.MustCompile("'SHOW' 'INDEX'")},
				{name: "show_keys", stmt: "show_stmt", match: regexp.MustCompile("'SHOW' 'KEYS'")},
				{name: "show_tables", stmt: "show_stmt", inline: []string{"opt_from_var_name_clause"}, match: regexp.MustCompile("'SHOW' 'TABLES'")},
				{name: "show_timezone", stmt: "show_stmt", match: regexp.MustCompile("'SHOW' 'TIME' 'ZONE'")},
				{name: "show_transaction", stmt: "show_stmt", match: regexp.MustCompile("'SHOW' 'TRANSACTION'")},
				{name: "truncate_stmt", inline: []string{"opt_table", "relation_expr_list", "relation_expr"}},
				{name: "update_stmt", inline: []string{"relation_expr_opt_alias", "set_clause_list", "set_clause", "single_set_clause", "multiple_set_clause", "ctext_row", "ctext_expr_list", "ctext_expr", "from_clause", "from_list", "where_clause", "returning_clause"}},
				{name: "values", stmt: "values_clause", inline: []string{"ctext_row", "ctext_expr_list", "ctext_expr"}},
			}

			for _, spec := range specs {
				wg.Add(1)
				go func(s stmtSpec) {
					defer wg.Done()
					if s.stmt == "" {
						s.stmt = s.name
					}
					g, err := runParse(br(), s.inline, s.stmt, false, s.match, s.exclude)
					if err != nil {
						log.Fatal(err)
					}
					for from, to := range s.replace {
						g = bytes.Replace(g, []byte(from), []byte(to), -1)
					}
					rr, err := runRR(bytes.NewReader(g))
					if err != nil {
						log.Fatal(err)
					}
					body, err := extract.ExtractTag(bytes.NewReader(rr), "svg")
					if err != nil {
						log.Fatal(err)
					}
					body = strings.Replace(body, `<a xlink:href="#`, `<a xlink:href="sql-grammar.html#`, -1)
					name := strings.Replace(s.name, "_stmt", "", 1)
					if err := ioutil.WriteFile(filepath.Join(baseDir, fmt.Sprintf("%s.html", name)), []byte(body), 0644); err != nil {
						log.Fatal(err)
					}
				}(spec)
			}
			wg.Wait()
		},
	}

	rootCmd.Flags().StringVar(&addr, "addr", "https://raw.githubusercontent.com/cockroachdb/cockroach/master/sql/parser/sql.y", "Location of sql.y file. Can also specify a local file.")
	rootCmd.Flags().StringVar(&baseDir, "base", filepath.Join("..", "_includes", "sql", "diagrams"), "Base directory for html output.")

	rootCmd.AddCommand(cmdBNF, cmdParse, cmdRR, cmdBody, cmdFuncs)
	rootCmd.PersistentFlags().StringVar(&outputPath, "out", "", "Output path; stdout if empty.")
	rootCmd.PersistentFlags().StringVar(&inputPath, "in", "", "Input path; stdin if empty.")
	rootCmd.Execute()
}

type stmtSpec struct {
	name           string
	stmt           string // if unspecified, uses name
	inline         []string
	replace        map[string]string
	match, exclude *regexp.Regexp
}

func runBNF(addr string) ([]byte, error) {
	log.Printf("generate BNF: %s", addr)
	return extract.GenerateBNF(addr)
}

func runParse(r io.Reader, inline []string, topStmt string, descend bool, match, exclude *regexp.Regexp) ([]byte, error) {
	log.Printf("parse: %s, inline: %s, descend: %v", topStmt, inline, descend)
	g, err := extract.ParseGrammar(r)
	if err != nil {
		log.Fatal(err)
	}
	if err := g.Inline(inline...); err != nil {
		log.Fatal(err)
	}
	return g.ExtractProduction(topStmt, descend, match, exclude)
}

func runRR(r io.Reader) ([]byte, error) {
	log.Printf("generate railroad diagrams")
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}
	html, err := extract.GenerateRR(b)
	if err != nil {
		return nil, err
	}
	s, err := extract.XHTMLtoHTML(bytes.NewReader(html))
	return []byte(s), err
}
