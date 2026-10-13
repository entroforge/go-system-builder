package classifier

import (
	"reflect"
	"testing"
)

func TestShellEffectsRespectArgumentsAndSegments(t *testing.T) {
	for _, tc := range []struct {
		command string
		effect  Effect
		paths   []string
	}{
		{`rg 'python3|node|open("x", "w")' docs`, ProvenRead, nil},
		{`echo '--help'; python3 .claude/evidence/script.py`, UnknownEffects, nil},
		{`python3 .claude/evidence/script.py --help`, UnknownEffects, nil},
		{`python3 --help`, ProvenRead, nil},
		{`cp report.json .claude/evidence/report.json && head -4 .claude/evidence/report.json`, KnownWrites, []string{".claude/evidence/report.json"}},
		{`printf '%s' 'python3' | tee .claude/evidence/report.json | head -4`, KnownWrites, []string{".claude/evidence/report.json"}},
		{`echo harmless # comment` + "\n" + `rm product.go`, KnownWrites, []string{"product.go"}},
		{`echo "$(touch product.go)"`, UnknownEffects, nil},
		{"echo `touch product.go`", UnknownEffects, nil},
		{`env MODE=read python3 script.py`, UnknownEffects, nil},
		{`cd .claude/evidence && rm product.go`, UnknownEffects, nil},
		{`echo x > "$OUTPUT"`, UnknownEffects, nil},
		{`cat <<'EOF'` + "\nhello\nEOF", UnknownEffects, nil},
		{`rm .claude/evidence/a\ b`, KnownWrites, []string{".claude/evidence/a b"}},
		{`r''m product.go`, KnownWrites, []string{"product.go"}},
		{`echo 2> '/dev/null'`, ProvenRead, nil},
		{`echo 2>&1`, UnknownEffects, nil},
		{`sort -oproduct.go input`, UnknownEffects, nil},
		{`sort -ro product.go input`, UnknownEffects, nil},
		{`sort $FLAGS input`, UnknownEffects, nil},
		{`sort *`, UnknownEffects, nil},
		{`rg '*.ts' docs`, ProvenRead, nil},
		{`echo ''rm product.go`, ProvenRead, nil},
		{`uniq input product.go`, UnknownEffects, nil},
		{`printf '%n' 'a[$(touch product.go)]'`, UnknownEffects, nil},
		{`test -v 'a[$(touch product.go)]'`, UnknownEffects, nil},
		{`date 010112002030`, UnknownEffects, nil},
		{`file -cC -m magic`, UnknownEffects, nil},
		{`rg 'end$|${value}' docs`, ProvenRead, nil},
		{`sort --compress-program=evil input`, UnknownEffects, nil},
		{`printf -v PATH /tmp; cat data`, UnknownEffects, nil},
		{`date -s2030-01-01`, UnknownEffects, nil},
		{`rg --pre=script.py word .`, UnknownEffects, nil},
		{`go test ./...`, UnknownEffects, nil},
		{`bash --help -c 'rm product.go'`, UnknownEffects, nil},
		{`mv product.go .claude/evidence/product.go`, KnownWrites, []string{"product.go", ".claude/evidence/product.go"}},
	} {
		t.Run(tc.command, func(t *testing.T) {
			got := AnalyzeEffects(tc.command)
			if got.Effect != tc.effect {
				t.Fatalf("got %+v; want %s", got, tc.effect)
			}
			if tc.paths != nil && !reflect.DeepEqual(got.Paths, tc.paths) {
				t.Fatalf("paths=%v want %v", got.Paths, tc.paths)
			}
		})
	}
}
