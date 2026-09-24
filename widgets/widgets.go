// Package widgets collects optional fbiw widgets and registers them on import.
package widgets

import (
	"github.com/movsb/fbiw/widgets/list"
	"github.com/movsb/fbiw/widgets/table"
)

type (
	Table     = table.Table
	TableRow  = table.TableRow
	TableCell = table.TableCell
	HTMLList  = list.List
	ListItem  = list.Item
)
