// Package providerkeys keeps provider keys in the database, for admins who
// would rather not put them in the environment. Each key is sealed with
// AES-256-GCM, under a key derived from the master key, and bound to its
// provider's name, so that a key copied to another provider doesn't open.
package providerkeys
