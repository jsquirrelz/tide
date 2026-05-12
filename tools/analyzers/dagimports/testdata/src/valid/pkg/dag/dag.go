// Synthetic pkg/dag for the analysistest "valid" fixture — stdlib only,
// so the dagimports analyzer must report no diagnostics here. The package
// import path resolved by analysistest is "valid/pkg/dag", which contains
// "/pkg/dag" — so the analyzer's path filter does engage on this file.
package dag

import (
	"fmt"
	"sort"
)

var _ = fmt.Sprintf
var _ = sort.Strings
