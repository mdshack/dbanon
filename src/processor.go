package dbanon

import (
	"strings"

	"vitess.io/vitess/go/vt/sqlparser"
)

type LineProcessor struct {
	Mode     string
	Config   *Config
	Provider ProviderInterface
	Eav      *Eav
}

func NewLineProcessor(m string, c *Config, p ProviderInterface, e *Eav) *LineProcessor {
	return &LineProcessor{Mode: m, Config: c, Provider: p, Eav: e}
}

func (p LineProcessor) ProcessLine(s string) string {
	i := strings.Index(s, "INSERT")
	if i == 0 {
		return p.processInsert(s)
	}

	findNextTable(s)

	return s
}

func (p LineProcessor) processInsert(s string) string {
	stmt, err := sqlparser.Parse(s)
	if err != nil {
		return s
	}
	insert, ok := stmt.(*sqlparser.Insert)

	// This _shouldn't happen but the statement might not be an Insert
	// For example, it'll be nil if the binary charset introducer is foudn
	// https://github.com/blastrain/vitess-sqlparser/issues/25
	if !ok {
		return s
	}

	t, _ := insert.Table.TableName()
	table := t.Name.String()

	processor := p.Config.ProcessTable(table)

	if processor == "" && p.Mode == "anonymize" {
		return s
	}

	var attributeId string
	var result bool
	var dataType string

	var entityTypeId string

	rows := insert.Rows.(sqlparser.Values)
	for _, vt := range rows {
		for i, e := range vt {
			column := currentTable[i].Name

			if processor == "table" && p.Mode == "anonymize" {
				result, dataType = p.Config.ProcessColumn(table, column)

				if !result {
					continue
				}
			}

			switch v := e.(type) {
			case *sqlparser.Literal:
				if processor == "table" {
					v.Val = p.Provider.Get(dataType, &v.Val)
				} else {
					if column == "attribute_id" {
						attributeId = string(v.Val)
						if p.Mode == "anonymize" {
							result, dataType = p.Config.ProcessEav(table, attributeId)
						}
					}
					if column == "value" && result {
						v.Val = p.Provider.Get(dataType, &v.Val)
					}
					if p.Mode == "map-eav" {
						if column == "entity_type_id" {
							entityTypeId = string(v.Val)
						}
						if column == "entity_type_code" {
							p.Eav.entityMap[string(v.Val)] = entityTypeId
						}
						if column == "attribute_code" {
							for _, eavConfig := range p.Eav.Config.Eav {
								if p.Eav.entityMap[eavConfig.Name] == entityTypeId {
									for eavK, eavV := range eavConfig.Attributes {
										if eavK == string(v.Val) {
											eavConfig.Attributes[attributeId] = eavV
										}
									}
								}
							}
						}
					}
				}
			}
		}
	}

	return sqlparser.String(insert) + ";\n"
}
