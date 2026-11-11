package repair

import (
	"fmt"
	"testing"
)

func TestTrackedIndexProtectsAncestorsNotSimilarPrefixes(t *testing.T) {
	b := baselineBoundary{}
	b.addTracked("src/deep/code.go")
	for _, p := range []string{"src", "src/deep", "src/deep/code.go"} {
		if !b.hasTracked(p) {
			t.Fatalf("missing ownership: %s", p)
		}
	}
	for _, p := range []string{"sr", "src/deeper", "src/deep/code", "src/absent.go"} {
		if b.hasTracked(p) {
			t.Fatalf("invented ownership: %s", p)
		}
	}
}

func BenchmarkTrackedOwnership(b *testing.B) {
	for _, n := range []int{2000, 8000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			index := baselineBoundary{}
			paths := make([]string, n)
			for i := range paths {
				paths[i] = fmt.Sprintf("src/pkg%d/file.go", i)
				index.addTracked(paths[i])
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				for _, p := range paths {
					if !index.hasTracked(p) || !index.hasTracked("src") || index.hasTracked("missing") {
						b.Fatal("incorrect index")
					}
				}
			}
		})
	}
}
