package rds

import (
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go/service/rds/rdsiface"
)

type mockParameterGroupAdapter struct {
	svc              rdsiface.RDSAPI
	customPgroupName string
	returnErr        error
}

func (m *mockParameterGroupAdapter) ProvisionCustomParameterGroupIfNecessary(i *RDSInstance, d *dedicatedDBAdapter) (string, error) {
	if m.returnErr != nil {
		return "", m.returnErr
	}
	return m.customPgroupName, nil
}

func TestPrepareCreateDbInstanceInput(t *testing.T) {
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
				svc:              &mockRDSClient{},
			},
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
				svc:       &mockRDSClient{},
			},
			expectedErr: testErr,
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			params, err := prepareCreateDbInput(test.dbInstance, test.dbAdapter, "foobar", test.pGroupAdapter)
			if err != nil && test.expectedErr == nil {
				t.Errorf("unexpected error: %s", err)
			}
			if test.expectedErr == nil && *params.DBParameterGroupName != test.expectedGroupName {
				t.Fatalf("expected group name: %s, got: %s", test.expectedGroupName, *params.DBParameterGroupName)
			}
		})
	}
}

func TestPrepareModifyDbInstanceInput(t *testing.T) {
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
				svc:              &mockRDSClient{},
			},
			expectedGroupName: "foobar",
		},
		"expect error": {
			dbInstance: &RDSInstance{
				BinaryLogFormat: "ROW",
				DbType:          "mysql",
			},
			dbAdapter: &dedicatedDBAdapter{},
			pGroupAdapter: &mockParameterGroupAdapter{
				svc:       &mockRDSClient{},
				returnErr: testErr,
			},
			expectedErr: testErr,
		},
	}

	for name, test := range testCases {
		t.Run(name, func(t *testing.T) {
			params, err := prepareModifyDbInstanceInput(test.dbInstance, test.dbAdapter, test.pGroupAdapter)
			if err != nil && test.expectedErr == nil {
				t.Errorf("unexpected error: %s", err)
			}
			if test.expectedErr == nil && *params.DBParameterGroupName != test.expectedGroupName {
				t.Fatalf("expected group name: %s, got: %s", test.expectedGroupName, *params.DBParameterGroupName)
			}
		})
	}
}
