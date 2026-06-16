package service

import "fmt"

// NextEmployeeCode returns the next auto-generated employee code given the
// current highest numeric suffix among existing EMP-#### codes. The result is
// always zero-padded to at least 4 digits, e.g. NextEmployeeCode(0) -> "EMP-0001".
func NextEmployeeCode(maxSeq int32) string {
	return fmt.Sprintf("EMP-%04d", maxSeq+1)
}
