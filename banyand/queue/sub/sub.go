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

// Package sub implements the queue server.
package sub

import (
	"io"

	"github.com/pkg/errors"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	clusterv1 "github.com/apache/skywalking-banyandb/api/proto/banyandb/cluster/v1"
	"github.com/apache/skywalking-banyandb/pkg/bus"
)

func (s *server) Send(stream clusterv1.Service_SendServer) error {
	reply := func(writeEntity *clusterv1.SendRequest, err error, message string) {
		s.log.Error().Stringer("written", writeEntity).Err(err).Msg(message)
		if errResp := stream.Send(&clusterv1.SendResponse{
			MessageId: writeEntity.MessageId,
			Error:     message,
		}); errResp != nil {
			s.log.Err(errResp).Msg("failed to send response")
		}
	}
	ctx := stream.Context()
	var topic *bus.Topic
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		writeEntity, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			reply(writeEntity, err, "failed to receive message")
			continue
		}
		if writeEntity.Topic != "" && topic == nil {
			t := bus.UniTopic(writeEntity.Topic)
			topic = &t
		}
		if topic == nil {
			reply(writeEntity, err, "topic is empty")
			continue
		}
		listener := s.getListeners(*topic)
		if listener == nil {
			reply(writeEntity, err, "no listener found")
			continue
		}
		m := listener.Rev(bus.NewMessage(bus.MessageID(writeEntity.MessageId), writeEntity.Body))
		if m.Data() == nil {
			reply(writeEntity, err, "no response")
			continue
		}
		message, ok := m.Data().(proto.Message)
		if !ok {
			reply(writeEntity, err, "invalid response")
			continue
		}
		anyMessage, err := anypb.New(message)
		if err != nil {
			reply(writeEntity, err, "failed to marshal message")
			continue
		}
		if err := stream.Send(&clusterv1.SendResponse{
			MessageId: writeEntity.MessageId,
			Body:      anyMessage,
		}); err != nil {
			reply(writeEntity, err, "failed to send response")
			continue
		}
	}
}

func (s *server) Subscribe(topic bus.Topic, listener bus.MessageListener) error {
	s.listenersLock.Lock()
	defer s.listenersLock.Unlock()
	if _, ok := s.listeners[topic]; !ok {
		s.listeners[topic] = listener
		return nil
	}
	return errors.New("topic already exists")
}

func (s *server) getListeners(topic bus.Topic) bus.MessageListener {
	s.listenersLock.RLock()
	defer s.listenersLock.RUnlock()
	return s.listeners[topic]
}