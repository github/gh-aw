package jsondecoderunknownfields

import (
	"encoding/json"
	jsonalias "encoding/json"
	"strings"
)

func reportsDefaultDecoder(input string) error {
	var value struct{ Name string }
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	return decoder.Decode(&value)
}

func reportsAliasedDecoder(input string) error {
	var value any
	decoder := jsonalias.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	return decoder.Decode(&value)
}

func acceptsStrictDecoder(input string) error {
	var value struct{ Name string }
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.DisallowUnknownFields()
	return decoder.Decode(&value)
}

func reportsStrictnessEnabledAfterDecode(input string) error {
	var value struct{ Name string }
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	_ = decoder.Decode(&value)
	decoder.DisallowUnknownFields()
	return nil
}

func reportsReassignedDecoder(input string) error {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input))
	decoder.DisallowUnknownFields()
	decoder = json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	return decoder.Decode(&value)
}

func reportsInlineDecoder(input string) error {
	var value any
	return json.NewDecoder(strings.NewReader(input)).Decode(&value) // want `json.NewDecoder accepts unknown fields by default`
}

func suppressedDecoder(input string) error {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) //nolint:jsondecoderunknownfields
	return decoder.Decode(&value)
}

func reportsUnusedDecoder(input string) {
	json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
}

func unrelatedReader(input string) {
	_ = strings.NewReader(input)
}

type decoderHolder struct {
	decoder *json.Decoder
}

func strictField(input string) {
	var value any
	var holder decoderHolder
	holder.decoder = json.NewDecoder(strings.NewReader(input))
	holder.decoder.DisallowUnknownFields()
	_ = holder.decoder.Decode(&value)
}

func distinctFields(input string) {
	var first, second decoderHolder
	first.decoder = json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	second.decoder = json.NewDecoder(strings.NewReader(input))
	second.decoder.DisallowUnknownFields()
}

func strictDistinctFields(input string) {
	var first, second decoderHolder
	first.decoder = json.NewDecoder(strings.NewReader(input))
	second.decoder = json.NewDecoder(strings.NewReader(input))
	first.decoder.DisallowUnknownFields()
	second.decoder.DisallowUnknownFields()
}

func conditionalStrictness(input string, strict bool) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	if strict {
		decoder.DisallowUnknownFields()
	}
	_ = decoder.Decode(&value)
}

func deferredStrictness(input string) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	defer decoder.DisallowUnknownFields()
	_ = decoder.Decode(&value)
}

func asynchronousStrictness(input string) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	go decoder.DisallowUnknownFields()
	_ = decoder.Decode(&value)
}

func closureStrictness(input string) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	_ = func() { decoder.DisallowUnknownFields() }
	_ = decoder.Decode(&value)
}

func loopStrictness(input string, n int) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	for i := 0; i < n; i++ {
		decoder.DisallowUnknownFields()
	}
	_ = decoder.Decode(&value)
}

func strictAlias(input string) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input))
	alias := decoder
	alias.DisallowUnknownFields()
	_ = decoder.Decode(&value)
}

func decodeAliasTooEarly(input string) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	alias := decoder
	_ = alias.Decode(&value)
	decoder.DisallowUnknownFields()
}

func interfaceAliasTooEarly(input string) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	var alias interface{ Decode(any) error } = decoder
	_ = alias.Decode(&value)
	decoder.DisallowUnknownFields()
}

func strictInterfaceAlias(input string) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input))
	var alias interface{ DisallowUnknownFields() } = decoder
	alias.DisallowUnknownFields()
	_ = decoder.Decode(&value)
}

func reassignedAlias(input string) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	alias := decoder
	alias = json.NewDecoder(strings.NewReader(input))
	alias.DisallowUnknownFields()
	_ = decoder.Decode(&value)
}

func reassignedOriginal(input string) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input))
	alias := decoder
	decoder = json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	alias.DisallowUnknownFields()
	_ = alias.Decode(&value)
	_ = decoder.Decode(&value)
}

func pointerField(input string, holder *decoderHolder) {
	var value any
	(*holder).decoder = json.NewDecoder(strings.NewReader(input))
	holder.decoder.DisallowUnknownFields()
	_ = (*holder).decoder.Decode(&value)
}

func pointerFieldDecodeTooEarly(input string, holder *decoderHolder) {
	var value any
	holder.decoder = json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	_ = (*holder).decoder.Decode(&value)
	holder.decoder.DisallowUnknownFields()
}

func dereferencedDecoder(input string) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input))
	(*decoder).DisallowUnknownFields()
	_ = decoder.Decode(&value)
}

func dereferencedDecoderTooEarly(input string) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	_ = (*decoder).Decode(&value)
	decoder.DisallowUnknownFields()
}

func conditionalAlias(input string, condition bool) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	var alias *json.Decoder
	if condition {
		alias = decoder
	}
	if alias != nil {
		_ = alias.Decode(&value)
	}
	decoder.DisallowUnknownFields()
}

func conditionalReassignment(input string, condition bool, other *json.Decoder) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	if condition {
		decoder = other
	}
	decoder.DisallowUnknownFields()
	_ = decoder.Decode(&value)
}

func rangeReassignment(input string, others []*json.Decoder) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	alias := decoder
	for _, decoder = range others {
	}
	decoder.DisallowUnknownFields()
	_ = alias.Decode(&value)
}

func indirectReassignment(input string, other *json.Decoder) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	alias := decoder
	pointer := &decoder
	*pointer = other
	decoder.DisallowUnknownFields()
	_ = alias.Decode(&value)
}

func gotoSkipsStrictness(input string, condition bool) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	if condition {
		goto decode
	}
	decoder.DisallowUnknownFields()
decode:
	_ = decoder.Decode(&value)
}

func copiedDecoder(input string) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	copy := *decoder
	copy.DisallowUnknownFields()
	_ = decoder.Decode(&value)
}

func copiedDecoderTooEarly(input string) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	copy := *decoder
	_ = copy.Decode(&value)
	decoder.DisallowUnknownFields()
}

func overwrittenStrictDecoder(input string, other *json.Decoder) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	decoder.DisallowUnknownFields()
	*decoder = *other
	_ = decoder.Decode(&value)
}

func siblingFieldWrite(input string) {
	var value any
	holder := &struct {
		decoder *json.Decoder
		number  int
	}{}
	holder.decoder = json.NewDecoder(strings.NewReader(input))
	holder.number = 1
	holder.decoder.DisallowUnknownFields()
	_ = holder.decoder.Decode(&value)
}

func closureReturn(input string) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input))
	_ = func() int { return 1 }
	decoder.DisallowUnknownFields()
	_ = decoder.Decode(&value)
}

type embeddedHolder struct {
	decoderHolder
}

func promotedField(input string) {
	var value any
	var holder embeddedHolder
	holder.decoder = json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	_ = holder.decoderHolder.decoder.Decode(&value)
	holder.decoder.DisallowUnknownFields()
}

func strictPromotedField(input string) {
	var value any
	var holder embeddedHolder
	holder.decoder = json.NewDecoder(strings.NewReader(input))
	holder.decoderHolder.decoder.DisallowUnknownFields()
	_ = holder.decoder.Decode(&value)
}

func copiedHolderTooEarly(input string) {
	var value any
	var holder decoderHolder
	holder.decoder = json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	copy := holder
	_ = copy.decoder.Decode(&value)
	holder.decoder.DisallowUnknownFields()
}

func methodValueTooEarly(input string) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	decode := decoder.Decode
	_ = decode(&value)
	decoder.DisallowUnknownFields()
}

func helperDecode(decoder *json.Decoder, value *any) {
	_ = decoder.Decode(value)
}

func helperDecodeTooEarly(input string) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	helperDecode(decoder, &value)
	decoder.DisallowUnknownFields()
}

func helperDecodeSlice(decoders []*json.Decoder, value *any) {
	_ = decoders[0].Decode(value)
}

func wrappedHelperEscape(input string) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	helperDecodeSlice([]*json.Decoder{decoder}, &value)
	decoder.DisallowUnknownFields()
}

func resetDecoder(decoder, other *json.Decoder) {
	*decoder = *other
}

func helperResetsStrictness(input string, other *json.Decoder) {
	var value any
	decoder := json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	decoder.DisallowUnknownFields()
	resetDecoder(decoder, other)
	_ = decoder.Decode(&value)
}

func copiedHolderResetsStrictness(input string, other *json.Decoder) {
	var value any
	var holder decoderHolder
	holder.decoder = json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	holder.decoder.DisallowUnknownFields()
	copy := holder
	*copy.decoder = *other
	_ = holder.decoder.Decode(&value)
}

func fieldReassignedThroughPointer(input string, other *json.Decoder) {
	var value any
	var holder decoderHolder
	pointer := &holder
	holder.decoder = json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
	alias := holder.decoder
	pointer.decoder = other
	holder.decoder.DisallowUnknownFields()
	_ = alias.Decode(&value)
}

func breakSkipsStrictness(input string, skip bool) {
	var value any
	var decoder *json.Decoder
	for {
		decoder = json.NewDecoder(strings.NewReader(input)) // want `json.NewDecoder accepts unknown fields by default`
		if skip {
			break
		}
		decoder.DisallowUnknownFields()
		break
	}
	_ = decoder.Decode(&value)
}
