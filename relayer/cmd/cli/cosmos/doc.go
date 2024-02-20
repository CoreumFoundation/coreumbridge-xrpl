// Package cosmos contains code copied from Cosmos SDK and modified by us.
// Package keys has been copied from https://github.com/cosmos/cosmos-sdk/tree/v0.47.5/client/keys
//
// Nothing has been removed, even if we don't need some functions. It makes future upgrades simpler.
// If we need to override sth, our version is placed in the `override` package and the only modifications
// to the original code are made to reference the overridden implementation. Whatever we override,
// the link to the original implementation is provided. Once sth is overridden, all the references
// should be updated to point to the same implementation.
package cosmos
