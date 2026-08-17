package application

// IDGenerator creates opaque local record identities.
type IDGenerator func() (string, error)
