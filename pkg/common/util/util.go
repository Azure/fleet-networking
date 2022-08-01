/*
Copyright (c) Microsoft Corporation.
Licensed under the MIT license.
*/

package util

// BeforeProgramExitWithError is collection of deferred functions of the main function and return a non-zero value.
// `os.Exit` does not honor `defer`, so we wrap the deferred function(s) in `BeforeProgramExitWithError`,
// and pass it to `os.Exit`.
// Panic can also be used instead to honour `defer`, but `os.Exit` is to follow the pattern of
// the package controller-runtime.
func BeforeProgramExitWithError(f func()) int {
	f()
	return 1
}
