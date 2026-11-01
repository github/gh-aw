package writebytestring

import "bytes"

func badParenConversion() {
	var buf bytes.Buffer
	s := "hello"
	buf.Write(([]byte(s))) // want `buf\.Write\(\[\]byte\(s\)\) can be replaced with io\.WriteString\(&buf, s\) to potentially avoid a \[\]byte allocation if the writer implements io\.StringWriter`
}
