package virtuoso

// Compliance events: where a device departs from the User Guide. The
// connector keeps running and counts the deviation; the count is logged
// at Disconnect and readable from ComplianceProfile.
const (
	// NoInfoObject: an answer without "info", which the guide says is always there.
	NoInfoObject = "virtuoso_no_info_object"
	// StatusUnknown: info.status neither "ok" nor "failed". Still an error to the caller.
	StatusUnknown = "virtuoso_status_unknown"
	// FailedWithoutErrors: "failed" with no entry in info.errors.
	FailedWithoutErrors = "virtuoso_failed_without_errors"
	// ErrorsWithOK: "ok" with an entry in info.errors. The entries are logged.
	ErrorsWithOK = "virtuoso_errors_with_ok"
	// AnswerNotJSON: a 200 whose body is not a JSON envelope.
	AnswerNotJSON = "virtuoso_answer_not_json"
	// NoSessionCookie: a login that succeeded and set no NEVION_SESID.
	NoSessionCookie = "virtuoso_no_session_cookie"
	// DataNotInSchema: the data document holds a value the schema does not declare.
	DataNotInSchema = "virtuoso_data_not_in_schema"
	// UnresolvedRef: the schema holds a $ref that does not resolve.
	UnresolvedRef = "virtuoso_schema_unresolved_ref"
	// ValueOutsideEnum: a field holds a number its schema enum does not list.
	ValueOutsideEnum = "virtuoso_value_outside_enum"
	// NodeNotInAnswer: an answer that does not carry the path that was asked for.
	NodeNotInAnswer = "virtuoso_node_not_in_answer"
)
