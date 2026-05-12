// Synthetic pkg/dag for the analysistest violation fixture; the
// dagimports analyzer must flag the forbidden k8s.io import below per
// the directive on the same line as that import.
package dag

import (
	"fmt"

	_ "k8s.io/apimachinery/pkg/runtime" // want `DAG-05 violation: forbidden import "k8s.io/apimachinery/pkg/runtime"`
)

var _ = fmt.Sprintf
