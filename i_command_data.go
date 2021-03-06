package ravendb

// ICommandData represents command data
type ICommandData interface {
	getId() string
	getName() string
	getChangeVector() *string
	getType() CommandType
	serialize(conventions *DocumentConventions) (interface{}, error)
}

// CommandData describes common data for commands
type CommandData struct {
	ID           string
	Name         string
	ChangeVector *string
	Type         CommandType
}

func (d *CommandData) getId() string {
	return d.ID
}

func (d *CommandData) getName() string {
	return d.Name
}

func (d *CommandData) getType() string {
	return d.Type
}

func (d *CommandData) getChangeVector() *string {
	return d.ChangeVector
}

func (d *CommandData) baseJSON() map[string]interface{} {
	res := map[string]interface{}{
		"Id":           d.ID,
		"Type":         d.Type,
		"ChangeVector": d.ChangeVector,
	}
	return res
}
