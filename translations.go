package gtfs

import (
	"fmt"

	"github.com/OneBusAway/go-gtfs/csv"
	"github.com/OneBusAway/go-gtfs/warnings"
)

// Translation is one row of translations.txt: the translation of one field
// into one language. It selects what it translates either by RecordId (and,
// for stop_times, RecordSubId, the stop_sequence) or by FieldValue, the
// field's exact original value. Rows for feed_info set neither.
//
// The IDs are kept as strings, since a row may refer to a table this package
// does not read. Use a Translator to look translations up.
type Translation struct {
	TableName   string
	FieldName   string
	Language    string
	Translation string
	RecordId    string
	RecordSubId string
	FieldValue  string
}

// parseTranslations reads translations.txt. A row is skipped with a warning
// if a required value is missing or it does not select its record in exactly
// one of the ways GTFS allows. Table, field and language names are interned,
// since a large feed repeats a few of them across many rows.
func parseTranslations(csv *csv.File) ([]Translation, []warnings.StaticWarning) {
	tableNameColumn := csv.RequiredColumn("table_name")
	fieldNameColumn := csv.RequiredColumn("field_name")
	languageColumn := csv.RequiredColumn("language")
	translationColumn := csv.RequiredColumn("translation")
	recordIDColumn := csv.OptionalColumn("record_id")
	recordSubIDColumn := csv.OptionalColumn("record_sub_id")
	fieldValueColumn := csv.OptionalColumn("field_value")

	if w := checkForMissingColumns(csv); len(w) > 0 {
		return nil, w
	}

	var translations []Translation
	var w []warnings.StaticWarning
	names := stringInterner{}
	for csv.NextRow() {
		t := Translation{
			TableName:   tableNameColumn.Read(),
			FieldName:   fieldNameColumn.Read(),
			Language:    languageColumn.Read(),
			Translation: translationColumn.Read(),
			RecordId:    recordIDColumn.Read(),
			RecordSubId: recordSubIDColumn.Read(),
			FieldValue:  fieldValueColumn.Read(),
		}
		var reason string
		switch missingKeys := csv.MissingRowKeys(); {
		case len(missingKeys) > 0:
			reason = "missing values " + fmt.Sprint(missingKeys)
		case t.TableName == "feed_info":
			if t.RecordId != "" || t.RecordSubId != "" || t.FieldValue != "" {
				reason = "feed_info rows cannot set record_id, record_sub_id or field_value"
			}
		case t.RecordId != "" && t.FieldValue != "":
			reason = "record_id and field_value are both set"
		case t.RecordId == "" && t.FieldValue == "":
			reason = "neither record_id nor field_value is set"
		case t.FieldValue != "" && t.RecordSubId != "":
			reason = "record_sub_id is set with field_value"
		case t.TableName == "stop_times" && t.RecordId != "" && t.RecordSubId == "":
			reason = "stop_times record_id needs a record_sub_id"
		}
		if reason != "" {
			w = append(w, warnings.NewStaticWarning(csv, warnings.TranslationInvalid{Reason: reason}))
			continue
		}
		t.TableName = names.intern(t.TableName)
		t.FieldName = names.intern(t.FieldName)
		t.Language = names.intern(t.Language)
		translations = append(translations, t)
	}
	return translations, w
}

// Translator looks up translations by the record and value they apply to.
type Translator struct {
	byRecord map[translationKey]string
	byValue  map[translationKey]string
}

type translationKey struct {
	table, field, language string
	// recordID and recordSubID, or value, depending on the map.
	recordID, recordSubID, value string
}

// NewTranslator indexes translations for lookup. If two translations select
// the same record or value, the first is used.
func NewTranslator(translations []Translation) *Translator {
	t := &Translator{byRecord: map[translationKey]string{}, byValue: map[translationKey]string{}}
	for _, tr := range translations {
		m, key := t.byRecord, translationKey{table: tr.TableName, field: tr.FieldName, language: tr.Language}
		if tr.FieldValue != "" {
			m, key.value = t.byValue, tr.FieldValue
		} else {
			key.recordID, key.recordSubID = tr.RecordId, tr.RecordSubId
		}
		if _, ok := m[key]; !ok {
			m[key] = tr.Translation
		}
	}
	return t
}

// Translate returns the translation into language of field in table for one
// record, identified as translations.txt does (recordSubID is the
// stop_sequence for stop_times and empty otherwise; both IDs are empty for
// feed_info). value is the field's original value. A translation selected by
// record ID takes precedence over one selected by value, as GTFS requires.
func (t *Translator) Translate(table, field, language, recordID, recordSubID, value string) (string, bool) {
	key := translationKey{table: table, field: field, language: language, recordID: recordID, recordSubID: recordSubID}
	if s, ok := t.byRecord[key]; ok {
		return s, true
	}
	if value == "" {
		return "", false
	}
	s, ok := t.byValue[translationKey{table: table, field: field, language: language, value: value}]
	return s, ok
}
