package gtfs

import (
	"fmt"
	"slices"

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
// one of the ways GTFS allows. Every value is interned: a large feed repeats
// most of them across many rows, such as one headsign for each trip using it.
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
	values := stringInterner{}
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
			reason = fmt.Sprintf("missing values %s", missingKeys)
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
		for _, f := range []*string{&t.TableName, &t.FieldName, &t.Language, &t.Translation, &t.RecordId, &t.RecordSubId, &t.FieldValue} {
			*f = values.intern(*f)
		}
		translations = append(translations, t)
	}
	// Drop the slack append left, which on a large file is many megabytes.
	return slices.Clone(translations), w
}

// Translator looks up translations by the record and value they apply to.
type Translator struct {
	translations map[translationKey]string
}

// translationKey selects by record IDs, or by a non-empty value with empty
// IDs, so the two kinds of key never collide.
type translationKey struct {
	table, field, language       string
	recordID, recordSubID, value string
}

// NewTranslator indexes translations for lookup. If two translations select
// the same record or value, the first is used.
func NewTranslator(translations []Translation) *Translator {
	t := &Translator{translations: make(map[translationKey]string, len(translations))}
	for _, tr := range translations {
		key := translationKey{table: tr.TableName, field: tr.FieldName, language: tr.Language}
		if tr.FieldValue != "" {
			key.value = tr.FieldValue
		} else {
			key.recordID, key.recordSubID = tr.RecordId, tr.RecordSubId
		}
		if _, ok := t.translations[key]; !ok {
			t.translations[key] = tr.Translation
		}
	}
	return t
}

// TranslationLookup names the field of one record to translate.
type TranslationLookup struct {
	TableName string
	FieldName string
	Language  string
	// RecordId and RecordSubId identify the record as translations.txt
	// does: for stop_times, the trip_id and stop_sequence; for feed_info,
	// both empty.
	RecordId    string
	RecordSubId string
	// FieldValue is the field's original value, for translations that
	// select by value.
	FieldValue string
}

// Translate returns the translation that applies to the field in q. A
// translation selected by record ID takes precedence over one selected by
// value, as GTFS requires.
func (t *Translator) Translate(q TranslationLookup) (string, bool) {
	key := translationKey{table: q.TableName, field: q.FieldName, language: q.Language, recordID: q.RecordId, recordSubID: q.RecordSubId}
	if s, ok := t.translations[key]; ok {
		return s, true
	}
	if q.FieldValue == "" {
		return "", false
	}
	s, ok := t.translations[translationKey{table: q.TableName, field: q.FieldName, language: q.Language, value: q.FieldValue}]
	return s, ok
}
