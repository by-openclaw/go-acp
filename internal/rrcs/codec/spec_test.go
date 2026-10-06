package codec

// Documents copied from the specification, 7.40 Rev 2, as printed —
// line breaks and indentation included. Tests decode these; they are
// the oracle until captures from a real RRCS exist.

// §5.6.1 "XML-RPC structure".
const specCallExample = `<?xml version="1.0"?>
<methodCall>
<methodName>examples.getStateName</methodName>
<params>
<param>
<value><i4>41</i4></value>
</param>
</params>
</methodCall>`

// §5.6.5 "XML-RPC Response".
const specResponseExample = `<?xml version="1.0"?>
<methodResponse>
  <params>
  <param>
  <value><string>South Dakota</string></value>
  </param>
  </params>
  </methodResponse>`

// §5.6.5 "Fault example".
const specFaultExample = `<?xml version="1.0"?>
<methodResponse>
  <fault>
  <value>
  <struct>
  <member>
  <name>faultCode</name>
  <value><int>4</int></value>
  </member>
  <member>
  <name>faultString</name>
  <value><string>Too many parameters.</string></value>
  </member>
  </struct>
  </value>
  </fault>
</methodResponse>`

// §5.6.3, the two-element struct.
const specStructExample = `<value><struct>
  <member>
  <name>lowerBound</name>
  <value><i4>18</i4></value>
  </member>
  <member>
  <name>upperBound</name>
  <value><i4>139</i4></value>
  </member>
  </struct></value>`

// §5.6.4, the four-element array.
const specArrayExample = `<value><array>
  <data>
  <value><i4>12</i4></value>
  <value><string>Egypt</string></value>
  <value><boolean>0</boolean></value>
  <value><i4>-31</i4></value>
  </data>
  </array></value>`

// §11.1 "Example for an incoming request to RRCS": SetXp.
const specSetXpCall = `<?xml version="1.0"?>
<methodCall>
  <methodName>SetXp</methodName>
  <params>
     <param><value><string>C0002817191</string></value></param>
     <param><value><int>1</int></value></param>
     <param><value><int>2</int></value></param>
     <param><value><int>19</int></value></param>
     <param><value><int>1</int></value></param>
     <param><value><int>5</int></value></param>
     <param><value><int>3</int></value></param>
  </params>
</methodCall>`

// §11.1, its response.
const specSetXpResponse = `<?xml version="1.0"?>
<methodResponse>
<params>
<param>
<value><array><data>
          <value><string>C0002817191</string></value>
          <value><int>0</int></value>
       </data></array></value>
     </param>
  </params>
</methodResponse>`

// §11.2 "Example for an outgoing request from RRCS": UpstreamFailed.
const specUpstreamFailedCall = `<?xml version="1.0"?>
<methodCall>
  <methodName>UpstreamFailed</methodName>
  <params>
     <param>
       <value><string>R1947584733</string></value>
     </param>
     <param>
       <value><int>2</int></value></param>
     </params>
</methodCall>`

// §6.3 TConferencePortAddress with the placeholders filled, keeping the
// member name exactly as printed: "UseSecondChannel " with its space.
const specConferencePortAddress = `<value><struct>
<member>
<name>Node</name>
<value><i4>2</i4></value>
</member>
<member>
<name>Port</name>
<value><i4>12</i4></value>
</member>
<member>
<name>UseSecondChannel </name>
<value><boolean>1</boolean></value>
</member>
<member>
<!-- talk right -->
<name>Talk</name>
<value><boolean>1</boolean></value>
</member>
<member>
<!-- listen right -->
<name>Listen</name>
 <value><boolean>0</boolean></value>
</member>
</struct></value>`

// §6.4 TMemberChangeList, one element, placeholders filled.
const specMemberChangeList = `<value><array><data>
<!-- 1st array element -->
<value><struct>
<member>
<name>AddToGroup</name>
<value><boolean>0</boolean></value>
</member>
<member>
<name>PortAddress</name>
<value><struct>
<member>
<name>IsInput</name>
<value><boolean>0</boolean></value>
      </member>
<member>
<name>Node</name>
<value><i4>3</i4></value>
</member>
<member>
<name>Port</name>
<value><i4>7</i4></value>
</member>
</struct></value>
</member>
<member>
<name>UseSecondChannel</name>
<value><boolean>1</boolean></value>
</member>
</struct></value>
<!-- additional array elements -->
</data></array></value>`

// specValue decodes one bare <value> document from the specification.
func specValue(doc string) (Value, error) {
	root, err := parseXML([]byte(doc))
	if err != nil {
		return Value{}, err
	}
	return parseValue(root)
}
