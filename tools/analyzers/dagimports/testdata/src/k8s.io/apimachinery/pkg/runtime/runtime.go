// Synthetic stub for k8s.io/apimachinery/pkg/runtime used only by the
// dagimports analysistest "violation" fixture. analysistest resolves
// imports via GOPATH-style lookups under testdata/src/, so the fixture's
// `_ "k8s.io/apimachinery/pkg/runtime"` import must be resolvable here.
// The package has no exported symbols; the analyzer only inspects the
// import string in the AST.
package runtime
