package securitytest

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/globocom/huskyCI/api/db"
	huskydocker "github.com/globocom/huskyCI/api/dockers"
	"github.com/globocom/huskyCI/api/log"
	"github.com/globocom/huskyCI/api/types"
	"github.com/globocom/huskyCI/api/util"
)

var securityTestAnalyze = map[string]func(scanInfo *SecTestScanInfo) error{
	"bandit":     analyzeBandit,
	"brakeman":   analyzeBrakeman,
	"enry":       analyzeEnry,
	"gitauthors": analyzeGitAuthors,
	"gosec":      analyzeGosec,
	"npmaudit":   analyzeNpmaudit,
	"yarnaudit":  analyzeYarnaudit,
	"safety":     analyzeSafety,
}

// SecTestScanInfo holds all information of securityTest scan.
type SecTestScanInfo struct {
	RID                   string
	URL                   string
	Branch                string
	SecurityTestName      string
	ErrorFound            error
	ReqNotFound           bool
	WarningFound          bool
	PackageNotFound       bool
	YarnLockNotFound      bool
	YarnErrorRunning      bool
	CommitAuthorsNotFound bool
	CommitAuthors         GitAuthorsOutput
	Codes                 []Code
	Container             types.Container
	FinalOutput           interface{}
	Vulnerabilities       types.HuskyCISecurityTestOutput
}

// New creates a new huskyCI scan based given RID, URL, Branch and a securityTest name and returns an error.
func (scanInfo *SecTestScanInfo) New(RID, URL, branch, securityTestName string) error {
	scanInfo.RID = RID
	scanInfo.URL = URL
	scanInfo.Branch = branch
	scanInfo.SecurityTestName = securityTestName
	return scanInfo.setSecurityTestContainer(securityTestName)
}

func (scanInfo *SecTestScanInfo) setSecurityTestContainer(securityTestName string) error {
	securityTestQuery := map[string]interface{}{"name": securityTestName}
	securityTest, err := db.FindOneDBSecurityTest(securityTestQuery)
	if err != nil {
		log.Error("createSecurityTestContainer", "SECURITYTEST", 2012, err)
		return err
	}
	scanInfo.Container.StartedAt = time.Now()
	scanInfo.Container.SecurityTest = securityTest
	return nil
}

// Start starts a new huskyCI scan!
func (scanInfo *SecTestScanInfo) Start() error {
	if err := scanInfo.dockerRun(scanInfo.Container.SecurityTest.TimeOutInSeconds); err != nil {
		scanInfo.ErrorFound = err
		scanInfo.prepareContainerAfterScan()
		return err
	}
	if err := scanInfo.analyze(); err != nil {
		scanInfo.ErrorFound = err
		scanInfo.prepareContainerAfterScan()
		return err
	}
	scanInfo.prepareContainerAfterScan()
	return nil
}

func (scanInfo *SecTestScanInfo) dockerRun(timeOutInSeconds int) error {
	image := scanInfo.Container.SecurityTest.Image
	imageTag := scanInfo.Container.SecurityTest.ImageTag
	fullContainerImage := fmt.Sprintf("%s:%s", image, imageTag)
	cmd := util.HandleCmd(scanInfo.URL, scanInfo.Branch, scanInfo.Container.SecurityTest.Cmd)
	finalCMD := util.HandlePrivateSSHKey(cmd)
	CID, cOutput, err := huskydocker.DockerRun(fullContainerImage, finalCMD, timeOutInSeconds)
	if err != nil {
		return err
	}
	scanInfo.Container.CID = CID
	scanInfo.Container.COutput = cOutput
	return nil
}

func (scanInfo *SecTestScanInfo) analyze() error {
	errorClonning := strings.Contains(scanInfo.Container.COutput, "ERROR_CLONING")
	if errorClonning {
		errorMsg := errors.New("error clonning")
		log.Error("analyze", "SECURITYTEST", 1031, scanInfo.URL, scanInfo.Branch, errorMsg)
		scanInfo.ErrorFound = errorMsg
		return errorMsg
	}
	securityTestAnalyze := securityTestAnalyze[scanInfo.SecurityTestName]
	return securityTestAnalyze(scanInfo)
}

func (scanInfo *SecTestScanInfo) prepareContainerAfterScan() {

	scanInfo.Container.FinishedAt = time.Now()
	scanInfo.Container.CInfo = "No issues found."
	scanInfo.Container.CResult = "passed"
	scanInfo.Container.CStatus = "finished"

	if scanInfo.ErrorFound != nil {
		scanInfo.Container.CInfo = "Error found running container"
		scanInfo.Container.CResult = "error"
		scanInfo.Container.CStatus = "error running"
		return
	}

	if scanInfo.ReqNotFound {
		scanInfo.Container.CInfo = "requeriments.txt was not found."
		scanInfo.Container.CResult = "warning"
		return
	}

	if scanInfo.PackageNotFound {
		scanInfo.Container.CInfo = "package-lock.json was not found."
		scanInfo.Container.CResult = "warning"
		return
	}

	if scanInfo.YarnLockNotFound {
		scanInfo.Container.CInfo = "yarn.lock was not found."
		scanInfo.Container.CResult = "warning"
		return
	}

	if scanInfo.CommitAuthorsNotFound {
		scanInfo.Container.CInfo = "Could not get authors. Probably master branch is being analyzed."
		return
	}

	if len(scanInfo.Vulnerabilities.MediumVulns) > 0 || len(scanInfo.Vulnerabilities.HighVulns) > 0 {
		scanInfo.Container.CInfo = "Issues found."
		scanInfo.Container.CResult = "failed"
	} else if len(scanInfo.Vulnerabilities.LowVulns) > 0 && (len(scanInfo.Vulnerabilities.MediumVulns) == 0 || len(scanInfo.Vulnerabilities.HighVulns) == 0) {
		scanInfo.Container.CInfo = "Warnings found."
		scanInfo.Container.CResult = "passed"
	}

}
