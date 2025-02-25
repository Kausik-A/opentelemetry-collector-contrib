// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package pdatautil // import "github.com/open-telemetry/opentelemetry-collector-contrib/internal/coreinternal/pdatautil"

import (
	"encoding/binary"
	"hash"
	"math"
	"sort"
	"sync"

	"github.com/cespare/xxhash/v2"
	"go.opentelemetry.io/collector/pdata/pcommon"
)

var (
	extraByte       = []byte{'\xf3'}
	keyPrefix       = []byte{'\xf4'}
	valEmpty        = []byte{'\xf5'}
	valBytesPrefix  = []byte{'\xf6'}
	valStrPrefix    = []byte{'\xf7'}
	valBoolTrue     = []byte{'\xf8'}
	valBoolFalse    = []byte{'\xf9'}
	valIntPrefix    = []byte{'\xfa'}
	valDoublePrefix = []byte{'\xfb'}
	valMapPrefix    = []byte{'\xfc'}
	valMapSuffix    = []byte{'\xfd'}
	valSlicePrefix  = []byte{'\xfe'}
	valSliceSuffix  = []byte{'\xff'}
)

type hashWriter struct {
	h       hash.Hash
	strBuf  []byte
	keysBuf []string
	sumHash []byte
	numBuf  []byte
}

func newHashWriter() *hashWriter {
	return &hashWriter{
		h:       xxhash.New(),
		strBuf:  make([]byte, 0, 128),
		keysBuf: make([]string, 0, 16),
		sumHash: make([]byte, 0, 16),
		numBuf:  make([]byte, 8),
	}
}

var hashWriterPool = &sync.Pool{
	New: func() interface{} { return newHashWriter() },
}

// MapHash return a hash for the provided map.
// Maps with the same underlying key/value pairs in different order produce the same deterministic hash value.
func MapHash(m pcommon.Map) [16]byte {
	hw := hashWriterPool.Get().(*hashWriter)
	defer hashWriterPool.Put(hw)
	hw.h.Reset()
	hw.writeMapHash(m)
	return hw.hashSum128()
}

// ValueHash return a hash for the provided pcommon.Value.
func ValueHash(v pcommon.Value) [16]byte {
	hw := hashWriterPool.Get().(*hashWriter)
	defer hashWriterPool.Put(hw)
	hw.h.Reset()
	hw.writeValueHash(v)
	return hw.hashSum128()
}

func (hw *hashWriter) writeMapHash(m pcommon.Map) {
	hw.keysBuf = hw.keysBuf[:0]
	m.Range(func(k string, v pcommon.Value) bool {
		hw.keysBuf = append(hw.keysBuf, k)
		return true
	})
	sort.Strings(hw.keysBuf)
	for _, k := range hw.keysBuf {
		v, _ := m.Get(k)
		hw.strBuf = hw.strBuf[:0]
		hw.strBuf = append(hw.strBuf, keyPrefix...)
		hw.strBuf = append(hw.strBuf, k...)
		hw.h.Write(hw.strBuf)
		hw.writeValueHash(v)
	}
}

func (hw *hashWriter) writeSliceHash(sl pcommon.Slice) {
	for i := 0; i < sl.Len(); i++ {
		hw.writeValueHash(sl.At(i))
	}
}

func (hw *hashWriter) writeValueHash(v pcommon.Value) {
	switch v.Type() {
	case pcommon.ValueTypeStr:
		hw.strBuf = hw.strBuf[:0]
		hw.strBuf = append(hw.strBuf, valStrPrefix...)
		hw.strBuf = append(hw.strBuf, v.Str()...)
		hw.h.Write(hw.strBuf)
	case pcommon.ValueTypeBool:
		if v.Bool() {
			hw.h.Write(valBoolTrue)
		} else {
			hw.h.Write(valBoolFalse)
		}
	case pcommon.ValueTypeInt:
		hw.h.Write(valIntPrefix)
		binary.LittleEndian.PutUint64(hw.numBuf, uint64(v.Int()))
		hw.h.Write(hw.numBuf)
	case pcommon.ValueTypeDouble:
		hw.h.Write(valDoublePrefix)
		binary.LittleEndian.PutUint64(hw.numBuf, math.Float64bits(v.Double()))
		hw.h.Write(hw.numBuf)
	case pcommon.ValueTypeMap:
		hw.h.Write(valMapPrefix)
		hw.writeMapHash(v.Map())
		hw.h.Write(valMapSuffix)
	case pcommon.ValueTypeSlice:
		hw.h.Write(valSlicePrefix)
		hw.writeSliceHash(v.Slice())
		hw.h.Write(valSliceSuffix)
	case pcommon.ValueTypeBytes:
		hw.h.Write(valBytesPrefix)
		hw.h.Write(v.Bytes().AsRaw())
	case pcommon.ValueTypeEmpty:
		hw.h.Write(valEmpty)
	}
}

// hashSum128 returns a [16]byte hash sum.
func (hw *hashWriter) hashSum128() [16]byte {
	b := hw.sumHash[:0]
	b = hw.h.Sum(b)

	// Append an extra byte to generate another part of the hash sum
	_, _ = hw.h.Write(extraByte)
	b = hw.h.Sum(b)

	res := [16]byte{}
	copy(res[:], b)
	return res
}
