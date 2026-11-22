package repository

import "github.com/perfect-panel/backend/internal/repository/kernel"

// The pagination helpers belong to the shared kernel; these re-export them.
const (
	DefaultPageSize = kernel.DefaultPageSize
	MaxPageSize     = kernel.MaxPageSize
)

// NormalizePage clamps pagination inputs; module repo implementations share it.
func NormalizePage(page, size int) (int, int) {
	return kernel.NormalizePage(page, size)
}

// NormalizePageFloor clamps pagination inputs without a minimum page size.
func NormalizePageFloor(page, size int) (int, int) {
	return kernel.NormalizePageFloor(page, size)
}
