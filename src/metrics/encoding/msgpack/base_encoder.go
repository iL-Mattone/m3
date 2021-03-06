// Copyright (c) 2016 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package msgpack

import (
	"github.com/m3db/m3/src/metrics/aggregation"
	"github.com/m3db/m3/src/metrics/metric/id"
	"github.com/m3db/m3/src/metrics/policy"
)

type encodeVarintFn func(value int64)
type encodeBoolFn func(value bool)
type encodeFloat64Fn func(value float64)
type encodeBytesFn func(value []byte)
type encodeBytesLenFn func(value int)
type encodeArrayLenFn func(value int)
type encodeStoragePolicyFn func(p policy.StoragePolicy)
type encodePolicyFn func(p policy.Policy)

// baseEncoder is the base encoder that provides common encoding APIs.
type baseEncoder struct {
	bufEncoder            BufferedEncoder
	encodeErr             error
	encodeVarintFn        encodeVarintFn
	encodeBoolFn          encodeBoolFn
	encodeFloat64Fn       encodeFloat64Fn
	encodeBytesFn         encodeBytesFn
	encodeBytesLenFn      encodeBytesLenFn
	encodeArrayLenFn      encodeArrayLenFn
	encodeStoragePolicyFn encodeStoragePolicyFn
	encodePolicyFn        encodePolicyFn
}

func newBaseEncoder(encoder BufferedEncoder) encoderBase {
	enc := &baseEncoder{bufEncoder: encoder}

	enc.encodeVarintFn = enc.encodeVarintInternal
	enc.encodeBoolFn = enc.encodeBoolInternal
	enc.encodeFloat64Fn = enc.encodeFloat64Internal
	enc.encodeBytesFn = enc.encodeBytesInternal
	enc.encodeBytesLenFn = enc.encodeBytesLenInternal
	enc.encodeArrayLenFn = enc.encodeArrayLenInternal
	enc.encodeStoragePolicyFn = enc.encodeStoragePolicyInternal
	enc.encodePolicyFn = enc.encodePolicyInternal

	return enc
}

func (enc *baseEncoder) encoder() BufferedEncoder                   { return enc.bufEncoder }
func (enc *baseEncoder) err() error                                 { return enc.encodeErr }
func (enc *baseEncoder) resetData()                                 { enc.bufEncoder.Reset() }
func (enc *baseEncoder) encodeVersion(version int)                  { enc.encodeVarint(int64(version)) }
func (enc *baseEncoder) encodeObjectType(objType objectType)        { enc.encodeVarint(int64(objType)) }
func (enc *baseEncoder) encodeNumObjectFields(numFields int)        { enc.encodeArrayLen(numFields) }
func (enc *baseEncoder) encodeRawID(id id.RawID)                    { enc.encodeBytes([]byte(id)) }
func (enc *baseEncoder) encodeVarint(value int64)                   { enc.encodeVarintFn(value) }
func (enc *baseEncoder) encodeBool(value bool)                      { enc.encodeBoolFn(value) }
func (enc *baseEncoder) encodeFloat64(value float64)                { enc.encodeFloat64Fn(value) }
func (enc *baseEncoder) encodeBytes(value []byte)                   { enc.encodeBytesFn(value) }
func (enc *baseEncoder) encodeBytesLen(value int)                   { enc.encodeBytesLenFn(value) }
func (enc *baseEncoder) encodeArrayLen(value int)                   { enc.encodeArrayLenFn(value) }
func (enc *baseEncoder) encodeStoragePolicy(p policy.StoragePolicy) { enc.encodeStoragePolicyFn(p) }
func (enc *baseEncoder) encodePolicy(p policy.Policy)               { enc.encodePolicyFn(p) }

func (enc *baseEncoder) reset(encoder BufferedEncoder) {
	enc.bufEncoder = encoder
	enc.encodeErr = nil
}

func (enc *baseEncoder) encodeChunkedID(id id.ChunkedID) {
	enc.encodeBytesLen(len(id.Prefix) + len(id.Data) + len(id.Suffix))
	enc.writeRaw(id.Prefix)
	enc.writeRaw(id.Data)
	enc.writeRaw(id.Suffix)
}

func (enc *baseEncoder) encodePolicyInternal(p policy.Policy) {
	enc.encodeNumObjectFields(numFieldsForType(policyType))
	enc.encodeStoragePolicyFn(p.StoragePolicy)
	enc.encodeCompressedAggregationTypes(p.AggregationID)
}

func (enc *baseEncoder) encodeCompressedAggregationTypes(aggTypes aggregation.ID) {
	if aggTypes.IsDefault() {
		enc.encodeNumObjectFields(numFieldsForType(defaultAggregationID))
		enc.encodeObjectType(defaultAggregationID)
		return
	}

	if aggregation.IDLen == 1 {
		enc.encodeNumObjectFields(numFieldsForType(shortAggregationID))
		enc.encodeObjectType(shortAggregationID)
		enc.encodeVarintFn(int64(aggTypes[0]))
		return
	}

	// NB(cw): Only reachable after we start to support more than 63 aggregation types
	enc.encodeNumObjectFields(numFieldsForType(longAggregationID))
	enc.encodeObjectType(longAggregationID)
	enc.encodeArrayLen(aggregation.IDLen)
	for _, v := range aggTypes {
		enc.encodeVarint(int64(v))
	}
}

func (enc *baseEncoder) encodeStoragePolicyInternal(p policy.StoragePolicy) {
	enc.encodeNumObjectFields(numFieldsForType(storagePolicyType))
	enc.encodeResolution(p.Resolution())
	enc.encodeRetention(p.Retention())
}

func (enc *baseEncoder) encodeResolution(resolution policy.Resolution) {
	if enc.encodeErr != nil {
		return
	}
	// If this is a known resolution, only encode its corresponding value.
	if resolutionValue, err := policy.ValueFromResolution(resolution); err == nil {
		enc.encodeNumObjectFields(numFieldsForType(knownResolutionType))
		enc.encodeObjectType(knownResolutionType)
		enc.encodeVarintFn(int64(resolutionValue))
		return
	}
	// Otherwise encode the entire resolution object.
	// TODO(xichen): validate the resolution before putting it on the wire.
	enc.encodeNumObjectFields(numFieldsForType(unknownResolutionType))
	enc.encodeObjectType(unknownResolutionType)
	enc.encodeVarintFn(int64(resolution.Window))
	enc.encodeVarintFn(int64(resolution.Precision))
}

func (enc *baseEncoder) encodeRetention(retention policy.Retention) {
	if enc.encodeErr != nil {
		return
	}
	// If this is a known retention, only encode its corresponding value.
	if retentionValue, err := policy.ValueFromRetention(retention); err == nil {
		enc.encodeNumObjectFields(numFieldsForType(knownRetentionType))
		enc.encodeObjectType(knownRetentionType)
		enc.encodeVarintFn(int64(retentionValue))
		return
	}
	// Otherwise encode the entire retention object.
	// TODO(xichen): validate the retention before putting it on the wire.
	enc.encodeNumObjectFields(numFieldsForType(unknownRetentionType))
	enc.encodeObjectType(unknownRetentionType)
	enc.encodeVarintFn(int64(retention))
}

// NB(xichen): the underlying msgpack encoder implementation
// always cast an integer value to an int64 and encodes integer
// values as varints, regardless of the actual integer type.
func (enc *baseEncoder) encodeVarintInternal(value int64) {
	if enc.encodeErr != nil {
		return
	}
	enc.encodeErr = enc.bufEncoder.EncodeInt64(value)
}

func (enc *baseEncoder) encodeBoolInternal(value bool) {
	if enc.encodeErr != nil {
		return
	}
	enc.encodeErr = enc.bufEncoder.EncodeBool(value)
}

func (enc *baseEncoder) encodeFloat64Internal(value float64) {
	if enc.encodeErr != nil {
		return
	}
	enc.encodeErr = enc.bufEncoder.EncodeFloat64(value)
}

func (enc *baseEncoder) encodeBytesInternal(value []byte) {
	if enc.encodeErr != nil {
		return
	}
	enc.encodeErr = enc.bufEncoder.EncodeBytes(value)
}

func (enc *baseEncoder) encodeBytesLenInternal(value int) {
	if enc.encodeErr != nil {
		return
	}
	enc.encodeErr = enc.bufEncoder.EncodeBytesLen(value)
}

func (enc *baseEncoder) encodeArrayLenInternal(value int) {
	if enc.encodeErr != nil {
		return
	}
	enc.encodeErr = enc.bufEncoder.EncodeArrayLen(value)
}

func (enc *baseEncoder) writeRaw(buf []byte) {
	if enc.encodeErr != nil {
		return
	}
	_, enc.encodeErr = enc.bufEncoder.Buffer().Write(buf)
}
