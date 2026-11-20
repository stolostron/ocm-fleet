package serialization

import (
	"database/sql"
	"encoding/json"

	"encodingvalues"
)

type PromotedEncoder struct {
	encodingvalues.ID
	Other encodingvalues.Unsupported
}

func EncodeSelectedReceiver(value PromotedEncoder) {
	json.Marshal(value) // want "Other.*does not declare json serialization"
	value.MarshalJSON()
	PromotedEncoder.MarshalJSON(value)
	saved := PromotedEncoder.MarshalJSON
	saved(value)
}

func DecodeBoxedValue(value encodingvalues.Unsupported) {
	var replaced any = value
	json.Unmarshal(nil, &replaced)
	var followed any = &value
	json.Unmarshal(nil, &followed) // want "does not declare json serialization"
	json.Marshal(&replaced)        // want "does not declare json serialization"
}

func EncodeWithoutSQLValue(db *sql.DB, value encodingvalues.ScanOnly) {
	db.Exec("SELECT ?", value) // want "does not declare sql serialization"
}
