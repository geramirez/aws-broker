package rds

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/18F/aws-broker/config"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/awserr"
	"github.com/aws/aws-sdk-go/service/rds"
	"github.com/aws/aws-sdk-go/service/rds/rdsiface"
)

var (
	pGroupPrefix = pGroupPrefixReal
)

// PgroupPrefix is the prefix for all pgroups created by the broker.
const pGroupPrefixReal = "cg-aws-broker-"
const pgCronLibraryName = "pg_cron"

type parameterGroupAdapterInterface interface {
	ProvisionCustomParameterGroupIfNecessary(
		i *RDSInstance,
		d *dedicatedDBAdapter,
	) (string, error)
}

type parameterGroupAdapter struct {
	svc rdsiface.RDSAPI
}

func (p *parameterGroupAdapter) getParameterGroupFamily(i *RDSInstance) error {
	if i.ParameterGroupFamily != "" {
		return nil
	}
	parameterGroupFamily := ""
	// If the DB version is not set (e.g., creating a new instance without
	// providing a specific version), determine the default parameter group
	// name from the default engine that will be chosen.
	if i.DbVersion == "" {
		dbEngineVersionsInput := &rds.DescribeDBEngineVersionsInput{
			DefaultOnly: aws.Bool(true),
			Engine:      aws.String(i.DbType),
		}

		// This call requires that the broker have permissions to make it.
		defaultEngineInfo, err := p.svc.DescribeDBEngineVersions(dbEngineVersionsInput)
		if err != nil {
			return err
		}

		// The value from the engine info is a string pointer, so we must
		// retrieve its actual value.
		parameterGroupFamily = *defaultEngineInfo.DBEngineVersions[0].DBParameterGroupFamily
	} else {
		// The DB instance has a version, therefore we can derive the
		// parameter group family directly.
		re := regexp.MustCompile(`^\d+\.*\d*`)
		dbversion := re.Find([]byte(i.DbVersion))
		parameterGroupFamily = i.DbType + string(dbversion)
	}
	i.ParameterGroupFamily = parameterGroupFamily
	return nil
}

func (p *parameterGroupAdapter) checkIfParameterGroupExists(pgroupName string) bool {
	dbParametersInput := &rds.DescribeDBParametersInput{
		DBParameterGroupName: aws.String(pgroupName),
		MaxRecords:           aws.Int64(20),
		Source:               aws.String("system"),
	}

	// If the db parameter group has already been created, we can return.
	_, err := p.svc.DescribeDBParameters(dbParametersInput)
	parameterGroupExists := (err == nil)
	if parameterGroupExists {
		log.Printf("%s parameter group already exists", pgroupName)
	}
	return parameterGroupExists
}

// This function will return the a custom parameter group with whatever custom
// parameters have been requested.  If there is no custom parameter group, it
// will be created.
func (p *parameterGroupAdapter) createOrModifyCustomParameterGroup(
	i *RDSInstance,
	customparams map[string]map[string]string,
) (string, error) {
	// i.FormatDBName() should always return the same value for the same database name,
	// so the parameter group name should remain consistent
	pgroupName := pGroupPrefix + i.FormatDBName()

	parameterGroupExists := p.checkIfParameterGroupExists(pgroupName)
	if !parameterGroupExists {
		// Otherwise, create a new parameter group in the proper family.
		err := p.getParameterGroupFamily(i)
		if err != nil {
			return "", fmt.Errorf("encounted error getting parameter group family: %w", err)
		}

		log.Printf("creating a parameter group named %s in the family of %s", pgroupName, i.ParameterGroupFamily)
		createInput := &rds.CreateDBParameterGroupInput{
			DBParameterGroupFamily: aws.String(i.ParameterGroupFamily),
			DBParameterGroupName:   aws.String(pgroupName),
			Description:            aws.String("aws broker parameter group for " + i.FormatDBName()),
		}

		_, err = p.svc.CreateDBParameterGroup(createInput)
		if err != nil {
			return "", fmt.Errorf("encounted error when creating database: %w", err)
		}
	}

	// iterate through the options and plug them into the parameter list
	parameters := []*rds.Parameter{}
	for k, v := range customparams[i.DbType] {
		parameters = append(parameters, &rds.Parameter{
			ApplyMethod:    aws.String("immediate"),
			ParameterName:  aws.String(k),
			ParameterValue: aws.String(v),
		})
	}

	// modify the parameter group we just created with the parameter list
	modifyinput := &rds.ModifyDBParameterGroupInput{
		DBParameterGroupName: aws.String(pgroupName),
		Parameters:           parameters,
	}
	_, err := p.svc.ModifyDBParameterGroup(modifyinput)
	if err != nil {
		return "", err
	}

	return pgroupName, nil
}

// This is here because the check is kinda big and ugly
func (p *parameterGroupAdapter) needCustomParameters(i *RDSInstance, s config.Settings) bool {
	// Currently, we only have one custom parameter for mysql, but if
	// we ever need to apply more, you can add them in here.
	if i.EnableFunctions &&
		s.EnableFunctionsFeature &&
		(i.DbType == "mysql") {
		return true
	}
	if i.BinaryLogFormat != "" &&
		(i.DbType == "mysql") {
		return true
	}
	if i.EnablePgCron &&
		(i.DbType == "postgres") {
		return true
	}
	return false
}

func (p *parameterGroupAdapter) getDefaultEngineParameter(paramName string, i *RDSInstance) (string, error) {
	err := p.getParameterGroupFamily(i)
	if err != nil {
		return "", err
	}
	describeEngDefaultParamsInput := &rds.DescribeEngineDefaultParametersInput{
		DBParameterGroupFamily: &i.ParameterGroupFamily,
	}
	for {
		result, err := p.svc.DescribeEngineDefaultParameters(describeEngDefaultParamsInput)
		if err != nil {
			return "", err
		}
		for _, param := range result.EngineDefaults.Parameters {
			if *param.ParameterName == paramName {
				log.Printf("found default parameter value %s for parameter %s", *param.ParameterValue, *param.ParameterName)
				return *param.ParameterValue, nil
			}
		}
		if result.EngineDefaults.Marker == nil || *result.EngineDefaults.Marker == "" {
			break
		}
		describeEngDefaultParamsInput.Marker = result.EngineDefaults.Marker
	}
	return "", nil
}

func (p *parameterGroupAdapter) buildCustomSharePreloadLibrariesParam(
	i *RDSInstance,
	customLibrary string,
) (string, error) {
	defaultSharedPreloadLibraries, err := p.getDefaultEngineParameter("shared_preload_libraries", i)
	if err != nil {
		return "", err
	}
	libraries := []string{
		customLibrary,
	}
	if defaultSharedPreloadLibraries != "" {
		libraries = append(libraries, defaultSharedPreloadLibraries)
	}
	return strings.Join(libraries, ","), nil
}

func (p *parameterGroupAdapter) getCustomParameters(
	i *RDSInstance,
	s config.Settings,
) (map[string]map[string]string, error) {
	customRDSParameters := make(map[string]map[string]string)

	if i.DbType == "mysql" {
		// enable functions
		customRDSParameters["mysql"] = make(map[string]string)
		if i.EnableFunctions && s.EnableFunctionsFeature {
			customRDSParameters["mysql"]["log_bin_trust_function_creators"] = "1"
		} else {
			customRDSParameters["mysql"]["log_bin_trust_function_creators"] = "0"
		}

		// set MySQL binary log format
		if i.BinaryLogFormat != "" {
			customRDSParameters["mysql"]["binlog_format"] = i.BinaryLogFormat
		}
	}

	if i.DbType == "postgres" {
		customRDSParameters["postgres"] = make(map[string]string)
		if i.EnablePgCron {
			preloadLibrariesParam, err := p.buildCustomSharePreloadLibrariesParam(i, pgCronLibraryName)
			if err != nil {
				return nil, err
			}
			customRDSParameters["postgres"]["shared_preload_libraries"] = preloadLibrariesParam
		}
	}

	return customRDSParameters, nil
}

func (p *parameterGroupAdapter) ProvisionCustomParameterGroupIfNecessary(
	i *RDSInstance,
	d *dedicatedDBAdapter,
) (string, error) {
	if !p.needCustomParameters(i, d.settings) {
		return "", nil
	}
	customRDSParameters, err := p.getCustomParameters(i, d.settings)
	if err != nil {
		return "", fmt.Errorf("encountered error getting custom parameters: %w", err)
	}

	// apply parameter group
	pgroupName, err := p.createOrModifyCustomParameterGroup(i, customRDSParameters)
	if err != nil {
		log.Println(err.Error())
		return "", fmt.Errorf("encountered error applying parameter group: %w", err)
	}
	return pgroupName, nil
}

// search out all the parameter groups that we created and try to clean them up
func cleanupCustomParameterGroups(svc rdsiface.RDSAPI) {
	input := &rds.DescribeDBParameterGroupsInput{}
	err := svc.DescribeDBParameterGroupsPages(input,
		func(pgroups *rds.DescribeDBParameterGroupsOutput, lastPage bool) bool {
			// If the pgroup matches the prefix, then try to delete it.
			// If it's in use, it will fail, so ignore that.
			for _, pgroup := range pgroups.DBParameterGroups {
				matched, err := regexp.Match("^"+pGroupPrefix, []byte(*pgroup.DBParameterGroupName))
				if err != nil {
					log.Printf("error trying to match %s in %s: %s", pGroupPrefix, *pgroup.DBParameterGroupName, err.Error())
				}
				if matched {
					deleteinput := &rds.DeleteDBParameterGroupInput{
						DBParameterGroupName: aws.String(*pgroup.DBParameterGroupName),
					}
					_, err := svc.DeleteDBParameterGroup(deleteinput)
					if err == nil {
						log.Printf("cleaned up %s parameter group", *pgroup.DBParameterGroupName)
					} else if err.(awserr.Error).Code() != "InvalidDBParameterGroupState" {
						// If you can't delete it because it's in use, that is fine.
						// The db takes a while to delete, so we will clean it up the
						// next time this is called.  Otherwise there is some sort of AWS error
						// and we should log that.
						log.Printf("There was an error cleaning up the %s parameter group.  The error was: %s", *pgroup.DBParameterGroupName, err.Error())
					}
				}
			}
			return true
		})
	if err != nil {
		log.Printf("Could not retrieve list of parameter groups while cleaning up: %s", err.Error())
		return
	}
}
