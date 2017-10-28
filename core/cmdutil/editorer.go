package cmdutil

import (
	"github.com/jmigpin/editor/core/toolbardata"
	"github.com/jmigpin/editor/ui"
)

type Editorer interface {
	Error(error)
	Errorf(string, ...interface{})
	UI() *ui.UI

	NewERowerBeforeRow(string, *ui.Column, *ui.Row) ERower
	ERowers() []ERower
	FindERower(string) (ERower, bool)
	ActiveERower() (ERower, bool)

	GoodColumnRowPlace() (col *ui.Column, next *ui.Row)

	HomeVars() *toolbardata.HomeVars
}
