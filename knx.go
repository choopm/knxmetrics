/*
Copyright 2024 Christoph Hoopmann

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package knxmetrics

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	v1 "github.com/choopm/knxrpc/knx/groupaddress/v1"
	"github.com/vapourismo/knx-go/knx/dpt"
	"go.opentelemetry.io/otel/metric"
)

// startKNXSubscriber runs a goroutine to subcsribe for values on the bus until s.ctx is done.
func (s *Server) startKNXSubscriber() error {
	// build slice of group addresses interested in
	groupAddresses := []string{}
	for _, mapping := range s.config.Mappings {
		groupAddresses = append(groupAddresses, mapping.KNXGroupAddress)
	}

	// connect stream for subscribed group addresses
	stream, err := s.rpcClient.Subscribe(s.ctx, connect.NewRequest(&v1.SubscribeRequest{
		GroupAddresses: groupAddresses,
		Event:          v1.Event_EVENT_UNSPECIFIED,
	}))
	if err != nil {
		return err
	}
	context.AfterFunc(s.ctx, func() { _ = stream.Close() }) // nolint:errcheck

	// start receiver loop
	for stream.Receive() {
		res := stream.Msg()

		// we only interested in response/write messages
		if res.Event != v1.Event_EVENT_RESPONSE &&
			res.Event != v1.Event_EVENT_WRITE {
			continue
		}

		// dispatch it
		err := s.dispatchBusMessage(res)
		if err != nil {
			return err
		}
	}
	if err := stream.Err(); err != nil &&
		!errors.Is(err, context.Canceled) {
		return fmt.Errorf("knxrpc stream closed: %s", stream.Err())
	}

	return nil
}

// dispatchBusMessage is called for every group address
func (s *Server) dispatchBusMessage(res *v1.SubscribeResponse) error {
	// find the mapping
	var mapping *Mapping
	for _, m := range s.config.Mappings {
		if res.GroupAddress == m.KNXGroupAddress {
			mapping = m
			break
		}
	}
	if mapping == nil {
		s.log.Warn().
			Str("group-address", res.GroupAddress).
			Msg("unknown group address")
		return nil
	}

	ev := s.log.Trace().
		Str("group-address", res.GroupAddress).
		Str("metric-name", mapping.MetricName)
	if len(mapping.MetricAttributes) > 0 {
		ev = ev.Interface("metric-attributes", mapping.MetricAttributes)
	}
	ev.Msg("received event")

	f := float64(0.0)

	i := mapping.MetricType
	switch {
	case i > 1000 && i < 2000: // 1xxx values observed are all mapped as bool
		if v := dpt.DPT_1001(false); v.Unpack(res.Data) == nil {
			if v {
				f = 1
			} else {
				f = 0
			}
		}
	case i == 5001: // percent 0..100%
		if v := dpt.DPT_5001(0); v.Unpack(res.Data) == nil {
			f = float64(v)
		}
	case i == 9001: // temperature C°
		if v := dpt.DPT_9001(0); v.Unpack(res.Data) == nil {
			f = float64(v)
		}
	case i == 9004: // Lux
		if v := dpt.DPT_9004(0); v.Unpack(res.Data) == nil {
			f = float64(v)
		}
	case i == 9005: // speed m/s
		if v := dpt.DPT_9005(0); v.Unpack(res.Data) == nil {
			f = float64(v)
		}
	default:
		s.log.Warn().
			Str("group-address", res.GroupAddress).
			Int("type", mapping.MetricType).
			Msg("unknown dpt type")
		return nil
	}

	mapping.gauge.Record(s.ctx, f, metric.WithAttributeSet(mapping.attributeSet))

	return nil
}
