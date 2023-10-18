// Copyright 2020 The Cockroach Authors.
//
// Use of this software is governed by the Business Source License
// included in the file licenses/BSL.txt.
//
// As of the Change Date specified in that file, in accordance with
// the Business Source License, use of this software will be governed
// by the Apache License, Version 2.0, included in the file
// licenses/APL.txt.

package schemachange

import (
	"context"

	"github.com/cockroachdb/cockroach/pkg/clusterversion"
	"github.com/cockroachdb/errors"
	"github.com/jackc/pgx/v5"
)

// opType is a enum to represent various types of "operations" that are
// supported by the schemachange workload. Each operation is mapped to a
// generator function via `opFuncs`.
//
//go:generate stringer -type=opType
type opType int

func init() {
	// Assert that every opType has a generator function in opFuncs and a weight
	// in opWeights.
	for op := opType(0); int(op) < numOpTypes; op++ {
		if len(opWeights) <= int(op) {
			panic(errors.AssertionFailedf(
				"no weight registered for %q (%d). Did you add an entry to opWeights?",
				op,
				op,
			))
		}
		if opFuncs[op] == nil {
			panic(errors.AssertionFailedf(
				"no generator function registered for %q (%d). Did you add an entry to opFuncs?",
				op,
				op,
			))
		}
	}

	// Sanity check that numOpTypes represents what we expect it to.
	if len(opFuncs) != numOpTypes {
		panic(errors.AssertionFailedf(
			"len(opFuncs) and numOpTypes don't match but a missing operation wasn't found. Did the definition of numOpTypes change?",
		))
	}
}

const (
	// Non-DDL operations

	insertRow  opType = iota // INSERT INTO <table> (<cols>) VALUES (<values>)
	selectStmt               // SELECT..
	validate                 // validate all table descriptors

	// DDL operations

	// Rename operations all rolled up into a single tree element and can't
	// easily be deduced by reflect so they're manually added here.

	renameIndex    // ALTER INDEX <table>@<index> RENAME TO <index>
	renameSequence // ALTER SEQUENCE <sequence> RENAME TO <sequence>
	renameTable    // ALTER TABLE <table> RENAME TO <table>
	renameView     // ALTER VIEW <view> RENAME TO <view>

	// The below list was generated by
	// https://gist.github.com/chrisseto/cd5f94c7e70cbbccd9df05788e4b1cb8 and
	// then hand curated.
	// To aid in book keeping:
	// Implemented commands are sorted alphabetically and then split into groupings.
	// Unimplemented commands are sorted alphabetically.
	// Disabled commands are handled by modifying opWeights.
	// All enabled commands will be run against the legacy schemachanger.
	// Commands may opt into being enabled in the declarative schemachanger by
	// adding an entry in opDeclarativeVersion.

	// ALTER DATABASE ...

	alterDatabaseAddRegion     // ALTER DATABASE <db> ADD REGION <region>
	alterDatabasePrimaryRegion //  ALTER DATABASE <db> PRIMARY REGION <region>
	alterDatabaseSurvivalGoal  // ALTER DATABASE <db> SURVIVE <failure_mode>

	// ALTER TABLE <table> ...

	alterTableAddColumn               // ALTER TABLE <table> ADD [COLUMN] <column> <type>
	alterTableAddConstraint           // ALTER TABLE <table> ADD CONSTRAINT <constraint> <def>
	alterTableAddConstraintForeignKey // ALTER TABLE <table> ADD CONSTRAINT <constraint> FOREIGN KEY (<column>) REFERENCES <table> (<column>)
	alterTableAddConstraintUnique     // ALTER TABLE <table> ADD CONSTRAINT <constraint> UNIQUE (<column>)
	alterTableAlterColumnType         // ALTER TABLE <table> ALTER [COLUMN] <column> [SET DATA] TYPE <type>
	alterTableAlterPrimaryKey         // ALTER TABLE <table> ALTER PRIMARY KEY USING COLUMNS (<columns>)
	alterTableDropColumn              // ALTER TABLE <table> DROP COLUMN <column>
	alterTableDropColumnDefault       // ALTER TABLE <table> ALTER [COLUMN] <column> DROP DEFAULT
	alterTableDropConstraint          // ALTER TABLE <table> DROP CONSTRAINT <constraint>
	alterTableDropNotNull             // ALTER TABLE <table> ALTER [COLUMN] <column> DROP NOT NULL
	alterTableDropStored              // ALTER TABLE <table> ALTER [COLUMN] <column> DROP STORED
	alterTableLocality                // ALTER TABLE <table> LOCALITY <locality>
	alterTableRenameColumn            // ALTER TABLE <table> RENAME [COLUMN] <column> TO <column>
	alterTableSetColumnDefault        // ALTER TABLE <table> ALTER [COLUMN] <column> SET DEFAULT <expr>
	alterTableSetColumnNotNull        // ALTER TABLE <table> ALTER [COLUMN] <column> SET NOT NULL

	// ALTER TYPE ...

	alterTypeDropValue // ALTER TYPE <type> DROP VALUE <value>

	// CREATE ...

	createTypeEnum // CREATE TYPE <type> ENUM AS <def>
	createIndex    // CREATE INDEX <index> ON <table> <def>
	createSchema   // CREATE SCHEMA <schema>
	createSequence // CREATE SEQUENCE <sequence> <def>
	createTable    // CREATE TABLE <table> <def>
	createTableAs  // CREATE TABLE <table> AS <def>
	createView     // CREATE VIEW <view> AS <def>

	// DROP ...

	dropIndex    // DROP INDEX <index>@<table>
	dropSchema   // DROP SCHEMA <schema>
	dropSequence // DROP SEQUENCE <sequence>
	dropTable    // DROP TABLE <table>
	dropView     // DROP VIEW <view>

	// Unimplemented operations. TODO(sql-foundations): Audit and/or implement these operations.
	// alterDatabaseAddSuperRegion
	// alterDatabaseAlterSuperRegion
	// alterDatabaseDropRegion
	// alterDatabaseDropSecondaryRegion
	// alterDatabaseDropSuperRegion
	// alterDatabaseOwner
	// alterDatabasePlacement
	// alterDatabaseSecondaryRegion
	// alterDatabaseSetZoneConfigExtension
	// alterDefaultPrivileges
	// alterFunctionDepExtension
	// alterFunctionOptions
	// alterFunctionRename
	// alterFunctionSetOwner
	// alterFunctionSetSchema
	// alterIndex
	// alterIndexPartitionBy
	// alterIndexVisible
	// alterRole
	// alterRoleSet
	// alterSchema
	// alterSchemaOwner
	// alterSchemaRename
	// alterSequence
	// alterTableInjectStats
	// alterTableOwner
	// alterTablePartitionByTable
	// alterTableRenameConstraint        // ALTER TABLE <table> RENAME CONSTRAINT <constraint> TO <constraint>
	// alterTableResetStorageParams
	// alterTableSetAudit
	// alterTableSetOnUpdate
	// alterTableSetSchema
	// alterTableSetStorageParams
	// alterTableSetVisible
	// alterTableValidateConstraint
	// alterType
	// alterTypeAddValue
	// alterTypeOwner
	// alterTypeRename
	// alterTypeRenameValue
	// alterTypeSetSchema
	// commentOnColumn
	// commentOnConstraint
	// commentOnDatabase
	// commentOnIndex
	// commentOnSchema
	// commentOnTable
	// createDatabase
	// createRole
	// createStats
	// createStatsOptions
	// createType
	// dropDatabase
	// dropFunction
	// dropOwnedBy
	// dropRole     // DROP ROLE <role>
	// dropType     // DROP TYPE <type>
	// grant
	// grantRole
	// grantTargetList
	// reassignOwnedBy
	// refreshMaterializedView
	// renameDatabase
	// reparentDatabase
	// revoke
	// revokeRole

	// numOpTypes contains the total number of opType entries and is used to
	// perform runtime assertions about various structures that aid in operation
	// generation.
	numOpTypes int = iota
)

var opFuncs = []func(*operationGenerator, context.Context, pgx.Tx) (*opStmt, error){
	// Non-DDL
	insertRow:  (*operationGenerator).insertRow,
	selectStmt: (*operationGenerator).selectStmt,
	validate:   (*operationGenerator).validate,

	// DDL Operations
	alterDatabaseAddRegion:            (*operationGenerator).addRegion,
	alterDatabasePrimaryRegion:        (*operationGenerator).primaryRegion,
	alterDatabaseSurvivalGoal:         (*operationGenerator).survive,
	alterTableAddColumn:               (*operationGenerator).addColumn,
	alterTableAddConstraint:           (*operationGenerator).addConstraint,
	alterTableAddConstraintForeignKey: (*operationGenerator).addForeignKeyConstraint,
	alterTableAddConstraintUnique:     (*operationGenerator).addUniqueConstraint,
	alterTableAlterColumnType:         (*operationGenerator).setColumnType,
	alterTableAlterPrimaryKey:         (*operationGenerator).alterTableAlterPrimaryKey,
	alterTableDropColumn:              (*operationGenerator).dropColumn,
	alterTableDropColumnDefault:       (*operationGenerator).dropColumnDefault,
	alterTableDropConstraint:          (*operationGenerator).dropConstraint,
	alterTableDropNotNull:             (*operationGenerator).dropColumnNotNull,
	alterTableDropStored:              (*operationGenerator).dropColumnStored,
	alterTableLocality:                (*operationGenerator).alterTableLocality,
	alterTableRenameColumn:            (*operationGenerator).renameColumn,
	alterTableSetColumnDefault:        (*operationGenerator).setColumnDefault,
	alterTableSetColumnNotNull:        (*operationGenerator).setColumnNotNull,
	alterTypeDropValue:                (*operationGenerator).dropTypeValue,
	createTypeEnum:                    (*operationGenerator).createEnum,
	createIndex:                       (*operationGenerator).createIndex,
	createSchema:                      (*operationGenerator).createSchema,
	createSequence:                    (*operationGenerator).createSequence,
	createTable:                       (*operationGenerator).createTable,
	createTableAs:                     (*operationGenerator).createTableAs,
	createView:                        (*operationGenerator).createView,
	dropIndex:                         (*operationGenerator).dropIndex,
	dropSchema:                        (*operationGenerator).dropSchema,
	dropSequence:                      (*operationGenerator).dropSequence,
	dropTable:                         (*operationGenerator).dropTable,
	dropView:                          (*operationGenerator).dropView,
	renameIndex:                       (*operationGenerator).renameIndex,
	renameSequence:                    (*operationGenerator).renameSequence,
	renameTable:                       (*operationGenerator).renameTable,
	renameView:                        (*operationGenerator).renameView,
}

var opWeights = []int{
	// Non-DDL
	insertRow:  0, // Disabled and tracked with #91863
	selectStmt: 10,
	validate:   2, // validate twice more often

	// DDL Operations
	alterTableAddColumn:               1,
	alterTableDropConstraint:          0, // TODO(spaskob): unimplemented
	alterTableAddConstraintForeignKey: 0, // Disabled and tracked with #91195
	alterDatabaseAddRegion:            1,
	alterTableAddConstraintUnique:     0,
	alterTableLocality:                1,
	createIndex:                       1,
	createSequence:                    1,
	createTable:                       1,
	createTableAs:                     1,
	createView:                        1,
	createTypeEnum:                    1,
	createSchema:                      1,
	alterTableDropColumn:              0,
	alterTableDropColumnDefault:       1,
	alterTableDropNotNull:             1,
	alterTableDropStored:              1,
	dropIndex:                         1,
	dropSequence:                      1,
	dropTable:                         1,
	dropView:                          1,
	alterTypeDropValue:                1,
	dropSchema:                        1,
	alterDatabasePrimaryRegion:        0, // Disabled and tracked with #83831
	alterTableRenameColumn:            1,
	renameIndex:                       1,
	renameSequence:                    1,
	renameTable:                       1,
	renameView:                        1,
	alterTableSetColumnDefault:        1,
	alterTableSetColumnNotNull:        1,
	alterTableAlterPrimaryKey:         1,
	alterTableAlterColumnType:         0, // Disabled and tracked with #66662.
	alterDatabaseSurvivalGoal:         0, // Disabled and tracked with #83831
}

// This workload will maintain its own list of minimal supported versions for
// the declarative schema changer, since the cluster we are running against can
// be downlevel. The declarative schema changer builder does have a supported
// list, but it's not sufficient for that reason.
var opDeclarativeVersion = map[opType]clusterversion.Key{
	alterTableAddColumn:               clusterversion.MinSupported,
	alterTableAddConstraintForeignKey: clusterversion.MinSupported,
	alterTableAddConstraintUnique:     clusterversion.MinSupported,
	alterTableDropColumn:              clusterversion.MinSupported,
	alterTableDropConstraint:          clusterversion.MinSupported,
	alterTableDropNotNull:             clusterversion.MinSupported,
	alterTypeDropValue:                clusterversion.MinSupported,
	createIndex:                       clusterversion.MinSupported,
	createSequence:                    clusterversion.MinSupported,
	createSchema:                      clusterversion.V23_2,
	dropIndex:                         clusterversion.MinSupported,
	dropSchema:                        clusterversion.MinSupported,
	dropSequence:                      clusterversion.MinSupported,
	dropTable:                         clusterversion.MinSupported,
	dropView:                          clusterversion.MinSupported,
}
