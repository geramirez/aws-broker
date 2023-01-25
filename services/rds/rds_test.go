package rds

import (
	"errors"
	"reflect"
	"testing"

	"github.com/18F/aws-broker/config"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/aws/aws-sdk-go/service/rds/rdsiface"
)

type mockRDSClient struct {
	rdsiface.RDSAPI
}

func (m *mockRDSClient) DescribeDBParameters(*rds.DescribeDBParametersInput) (*rds.DescribeDBParametersOutput, error) {
	return nil, nil
}

func (m *mockRDSClient) ModifyDBInstance(*rds.ModifyDBInstanceInput) (*rds.ModifyDBInstanceOutput, error) {
	return nil, nil
}

func (m *mockRDSClient) ModifyDBParameterGroup(*rds.ModifyDBParameterGroupInput) (*rds.DBParameterGroupNameMessage, error) {
	return nil, nil
}

type mockParameterGroupAdapter struct {
	customPgroupName string
	returnErr        error
}

func (m *mockParameterGroupAdapter) provisionCustomParameterGroupIfNecessary(i *RDSInstance, d *dedicatedDBAdapter, svc rdsiface.RDSAPI) (string, error) {
	if m.returnErr != nil {
		return "", m.returnErr
	}
	return m.customPgroupName, nil
}

func TestNeedCustomParameters(t *testing.T) {
	testCases := map[string]struct {
		dbInstance *RDSInstance
		settings   config.Settings
		expectedOk bool
	}{
		"default": {
			dbInstance: &RDSInstance{},
			settings:   config.Settings{},
			expectedOk: false,
		},
		"valid binary log format": {
			dbInstance: &RDSInstance{
				BinaryLogFormat: "ROW",
				DbType:          "mysql",
			},
			settings:   config.Settings{},
			expectedOk: true,
		},
		"valid binary log format, wrong database type": {
			dbInstance: &RDSInstance{
				BinaryLogFormat: "ROW",
				DbType:          "psql",
			},
			settings:   config.Settings{},
			expectedOk: false,
		},
		"instance functions enabled, settings disabled": {
			dbInstance: &RDSInstance{
				EnableFunctions: true,
				DbType:          "mysql",
			},
			settings: config.Settings{
				EnableFunctionsFeature: false,
			},
			expectedOk: false,
		},
		"instance functions disabled, settings enabled": {
			dbInstance: &RDSInstance{
				EnableFunctions: false,
				DbType:          "mysql",
			},
			settings: config.Settings{
				EnableFunctionsFeature: true,
			},
			expectedOk: false,
		},
		"instance functions enabled, settings enabled, wrong db type": {
			dbInstance: &RDSInstance{
				EnableFunctions: true,
				DbType:          "psql",
			},
			settings: config.Settings{
				EnableFunctionsFeature: true,
			},
			expectedOk: false,
		},
		"instance functions enabled, settings enabled": {
			dbInstance: &RDSInstance{
				EnableFunctions: true,
				DbType:          "mysql",
			},
			settings: config.Settings{
				EnableFunctionsFeature: true,
			},
			expectedOk: true,
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			if needCustomParameters(test.dbInstance, test.settings) != test.expectedOk {
				t.Fatalf("should be %v", test.expectedOk)
			}
		})
	}
}

func TestGetCustomParameters(t *testing.T) {
	testCases := map[string]struct {
		dbInstance     *RDSInstance
		settings       config.Settings
		expectedParams map[string]map[string]string
	}{
		"enabled functions": {
			dbInstance: &RDSInstance{
				EnableFunctions: true,
			},
			settings: config.Settings{
				EnableFunctionsFeature: true,
			},
			expectedParams: map[string]map[string]string{
				"mysql": {
					"log_bin_trust_function_creators": "1",
				},
			},
		},
		"instance functions disabled, settings enabled": {
			dbInstance: &RDSInstance{
				EnableFunctions: false,
			},
			settings: config.Settings{
				EnableFunctionsFeature: true,
			},
			expectedParams: map[string]map[string]string{
				"mysql": {
					"log_bin_trust_function_creators": "0",
				},
			},
		},
		"instance functions enabled, settings disabled": {
			dbInstance: &RDSInstance{
				EnableFunctions: true,
			},
			settings: config.Settings{
				EnableFunctionsFeature: false,
			},
			expectedParams: map[string]map[string]string{
				"mysql": {
					"log_bin_trust_function_creators": "0",
				},
			},
		},
		"binary log format": {
			dbInstance: &RDSInstance{
				BinaryLogFormat: "ROW",
			},
			settings: config.Settings{},
			expectedParams: map[string]map[string]string{
				"mysql": {
					"log_bin_trust_function_creators": "0",
					"binlog_format":                   "ROW",
				},
			},
		},
	}
	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			params := getCustomParameters(test.dbInstance, test.settings)
			if !reflect.DeepEqual(params, test.expectedParams) {
				t.Fatalf("expected %s, got: %s", test.expectedParams, params)
			}
		})
	}
}

func TestGetModifyDbInstanceInput(t *testing.T) {
	testErr := errors.New("fail")
	testCases := map[string]struct {
		dbInstance        *RDSInstance
		dbAdapter         *dedicatedDBAdapter
		pGroupAdapter     *mockParameterGroupAdapter
		svc               *mockRDSClient
		expectedGroupName string
		expectedErr       error
	}{
		"expect returned group name": {
			dbInstance: &RDSInstance{
				BinaryLogFormat: "ROW",
				DbType:          "mysql",
			},
			dbAdapter: &dedicatedDBAdapter{},
			pGroupAdapter: &mockParameterGroupAdapter{
				customPgroupName: "foobar",
			},
			svc:               &mockRDSClient{},
			expectedGroupName: "foobar",
		},
		"expect error": {
			dbInstance: &RDSInstance{
				BinaryLogFormat: "ROW",
				DbType:          "mysql",
			},
			dbAdapter: &dedicatedDBAdapter{},
			pGroupAdapter: &mockParameterGroupAdapter{
				returnErr: testErr,
			},
			svc:         &mockRDSClient{},
			expectedErr: testErr,
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			params, err := getModifyDbInstanceInput(test.dbInstance, test.dbAdapter, test.svc, test.pGroupAdapter)
			if err != nil && test.expectedErr == nil {
				t.Errorf("unexpected error: %s", err)
			}
			if test.expectedErr == nil && *params.DBParameterGroupName != test.expectedGroupName {
				t.Fatalf("expected group name: %s", test.expectedGroupName)
			}
		})
	}
}

// func TestProvisionCustomParameterGroupIfNecessary(t *testing.T) {
// 	i := &RDSInstance{
// 		BinaryLogFormat: "ROW",
// 		DbType:          "mysql",
// 	}
// 	d := &dedicatedDBAdapter{}
// 	svc := &mockRDSClient{}

// 	pGroupName, err := provisionCustomParameterGroupIfNecessary(i, svc)
// 	if err != nil {
// 		t.Errorf("unexpected error: %s", err)
// 	}
// 	if pGroupName == "" {
// 		t.Fatalf("expected group name")
// 	}
// }
