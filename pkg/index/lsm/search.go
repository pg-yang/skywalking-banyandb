// Licensed to Apache Software Foundation (ASF) under one or more contributor
// license agreements. See the NOTICE file distributed with
// this work for additional information regarding copyright
// ownership. Apache Software Foundation (ASF) licenses this file to you under
// the Apache License, Version 2.0 (the "License"); you may
// not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.

package lsm

import (
	"bytes"

	"github.com/pkg/errors"
	"go.uber.org/multierr"

	"github.com/apache/skywalking-banyandb/api/common"
	modelv2 "github.com/apache/skywalking-banyandb/api/proto/banyandb/model/v2"
	"github.com/apache/skywalking-banyandb/banyand/kv"
	"github.com/apache/skywalking-banyandb/pkg/convert"
	"github.com/apache/skywalking-banyandb/pkg/index"
	"github.com/apache/skywalking-banyandb/pkg/index/posting"
	"github.com/apache/skywalking-banyandb/pkg/index/posting/roaring"
)

func (s *store) MatchField(fieldKey index.FieldKey) (list posting.List, err error) {
	return s.Range(fieldKey, index.RangeOpts{})
}

func (s *store) MatchTerms(field index.Field) (list posting.List, err error) {
	f, err := field.Marshal(s.termMetadata)
	if err != nil {
		return nil, err
	}
	list = roaring.NewPostingList()
	err = s.lsm.GetAll(f, func(itemID []byte) error {
		list.Insert(common.ItemID(convert.BytesToUint64(itemID)))
		return nil
	})
	if errors.Is(err, kv.ErrKeyNotFound) {
		return roaring.EmptyPostingList, nil
	}
	return
}

func (s *store) Range(fieldKey index.FieldKey, opts index.RangeOpts) (list posting.List, err error) {
	iter, err := s.Iterator(fieldKey, opts, modelv2.QueryOrder_SORT_ASC)
	if err != nil {
		return roaring.EmptyPostingList, err
	}
	list = roaring.NewPostingList()
	for iter.Next() {
		err = multierr.Append(err, list.Union(iter.Val().Value))
	}
	err = multierr.Append(err, iter.Close())
	return
}

func (s *store) Iterator(fieldKey index.FieldKey, termRange index.RangeOpts, order modelv2.QueryOrder_Sort) (index.FieldIterator, error) {
	return index.NewFieldIteratorTemplate(fieldKey, termRange, order, s.lsm, s.termMetadata, func(term, value []byte, delegated kv.Iterator) (*index.PostingValue, error) {
		pv := &index.PostingValue{
			Term:  term,
			Value: roaring.NewPostingListWithInitialData(convert.BytesToUint64(value)),
		}

		for ; delegated.Valid(); delegated.Next() {
			f := index.Field{}
			err := f.Unmarshal(s.termMetadata, delegated.Key())
			if err != nil {
				return nil, err
			}
			if !bytes.Equal(f.Term, term) {
				break
			}
			pv.Value.Insert(common.ItemID(convert.BytesToUint64(delegated.Val())))
		}
		return pv, nil
	})
}