package metriccollector

import (
	"math"
	"strconv"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/pkg/errors"
	"github.com/yndd/ndd-yang/pkg/yparser"
)

func (c *metricCollector) handleSubscription(resp *gnmi.SubscribeResponse) error {
	targetName := c.GetTarget().Config.Name

	log := c.log.WithValues("Target", targetName)
	//log.Debug("handle target update from device")

	switch resp.GetResponse().(type) {
	case *gnmi.SubscribeResponse_Update:
		//log.Debug("handle target update from device", "Prefix", resp.GetUpdate().GetPrefix())

		// check if the target cache exists
		if !c.targetCache.GetCache().HasTarget(targetName) {
			log.Debug("handle target update target not found in cache")
			return errors.New("target cache does not exist")
		}
		//n := resp.GetUpdate()

		for _, u := range resp.GetUpdate().GetUpdate() {
			val, err := yparser.GetValue(u.GetVal())
			if err != nil {
				continue
			}
			// this is to filter out boolean
			_, err = getFloat(val)
			if err != nil {
				continue
			}

			n := &gnmi.Notification{
				Timestamp: resp.GetUpdate().GetTimestamp(),
				Prefix:    &gnmi.Path{Target: targetName},
				Update:    []*gnmi.Update{u},
			}

			//log.Debug("handle target update", "Path", yparser.GnmiPath2XPath(u.GetPath(), true), "Value", val)
			if err := c.targetCache.GnmiUpdate(targetName, n); err != nil {
				log.Debug("handle target update", "error", err, "Path", yparser.GnmiPath2XPath(u.GetPath(), true), "Value", u.GetVal())
				return errors.New("cache update failed")
			}
		}

	case *gnmi.SubscribeResponse_SyncResponse:
		log.Debug("SyncResponse")
	}

	return nil
}

func getFloat(v interface{}) (float64, error) {
	switch i := v.(type) {
	case float64:
		return float64(i), nil
	case float32:
		return float64(i), nil
	case int64:
		return float64(i), nil
	case int32:
		return float64(i), nil
	case int16:
		return float64(i), nil
	case int8:
		return float64(i), nil
	case uint64:
		return float64(i), nil
	case uint32:
		return float64(i), nil
	case uint16:
		return float64(i), nil
	case uint8:
		return float64(i), nil
	case int:
		return float64(i), nil
	case uint:
		return float64(i), nil
	case string:
		f, err := strconv.ParseFloat(i, 64)
		if err != nil {
			return math.NaN(), err
		}
		return f, err
	default:
		return math.NaN(), errors.New("getFloat: unknown value is of incompatible type")
	}
}
