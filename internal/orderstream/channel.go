package orderstream

const redisChannelPrefix = "order-events:"

// Channel returns the Redis Pub/Sub channel for one order's durable event
// stream. The channel is only a wake-up path; consumers still read events
// from the database by id.
func Channel(orderNo string) string {
	return redisChannelPrefix + orderNo
}
