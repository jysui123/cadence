// Copyright (c) 2017 Uber Technologies, Inc.
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package frontend

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/pborman/uuid"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/suite"
	"github.com/uber/cadence/.gen/go/shared"
	"github.com/uber/cadence/common"
	"github.com/uber/cadence/common/archiver/provider"
	"github.com/uber/cadence/common/cluster"
	"github.com/uber/cadence/common/log/loggerimpl"
	"github.com/uber/cadence/common/mocks"
	"github.com/uber/cadence/common/persistence"
	persistencetests "github.com/uber/cadence/common/persistence/persistence-tests"
	dc "github.com/uber/cadence/common/service/dynamicconfig"
)

type (
	domainHandlerGlobalDomainEnabledMasterClusterSuite struct {
		suite.Suite
		persistencetests.TestBase

		config               *Config
		logger               log.Logger
		metadataMgr          persistence.MetadataManager
		mockProducer         *mocks.KafkaProducer
		mockDomainReplicator DomainReplicator
		mockArchiverProvider *provider.ArchiverProviderMock

		handler *domainHandlerImpl
	}
)

func TestDomainHandlerGlobalDomainEnabledMasterClusterSuite(t *testing.T) {
	s := new(domainHandlerGlobalDomainEnabledMasterClusterSuite)
	suite.Run(t, s)
}

func (s *domainHandlerGlobalDomainEnabledMasterClusterSuite) SetupSuite() {
	if testing.Verbose() {
		log.SetOutput(os.Stdout)
	}

	s.TestBase = persistencetests.NewTestBaseWithCassandra(&persistencetests.TestBaseOptions{
		ClusterMetadata: cluster.GetTestClusterMetadata(true, true, false),
	})
	s.TestBase.Setup()
}

func (s *domainHandlerGlobalDomainEnabledMasterClusterSuite) TearDownSuite() {
	s.TestBase.TearDownWorkflowStore()
}

func (s *domainHandlerGlobalDomainEnabledMasterClusterSuite) SetupTest() {
	logger := loggerimpl.NewNopLogger()
	s.config = NewConfig(dc.NewCollection(dc.NewNopClient(), logger), numHistoryShards, false)
	s.metadataMgr = s.TestBase.MetadataProxy
	s.mockProducer = &mocks.KafkaProducer{}
	s.mockDomainReplicator = NewDomainReplicator(s.mockProducer, logger)
	s.mockArchiverProvider = &provider.ArchiverProviderMock{}

	s.handler = newDomainHandler(s.config, logger, s.metadataMgr, s.ClusterMetadata,
		s.mockDomainReplicator, s.mockArchiverProvider)
}

func (s *domainHandlerGlobalDomainEnabledMasterClusterSuite) TearDownTest() {
	s.mockProducer.AssertExpectations(s.T())
}

func (s *domainHandlerGlobalDomainEnabledMasterClusterSuite) TestRegisterGetDomain_LocalDomain_InvalidCluster() {
	domainName := s.getRandomDomainName()
	description := "some random description"
	email := "some random email"
	retention := int32(7)
	emitMetric := true
	activeClusterName := cluster.TestAlternativeClusterName
	clusters := []*shared.ClusterReplicationConfiguration{
		&shared.ClusterReplicationConfiguration{
			ClusterName: common.StringPtr(activeClusterName),
		},
	}
	data := map[string]string{"some random key": "some random value"}
	isGlobalDomain := false

	err := s.handler.registerDomain(context.Background(), &shared.RegisterDomainRequest{
		Name:                                   common.StringPtr(domainName),
		Description:                            common.StringPtr(description),
		OwnerEmail:                             common.StringPtr(email),
		WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(retention),
		EmitMetric:                             common.BoolPtr(emitMetric),
		Clusters:                               clusters,
		ActiveClusterName:                      common.StringPtr(activeClusterName),
		Data:                                   data,
		IsGlobalDomain:                         common.BoolPtr(isGlobalDomain),
	})
	s.IsType(&shared.BadRequestError{}, err)
}

func (s *domainHandlerGlobalDomainEnabledMasterClusterSuite) TestRegisterGetDomain_LocalDomain_AllDefault() {
	domainName := s.getRandomDomainName()
	isGlobalDomain := false
	var clusters []*shared.ClusterReplicationConfiguration
	for _, replicationConfig := range persistence.GetOrUseDefaultClusters(s.ClusterMetadata.GetCurrentClusterName(), nil) {
		clusters = append(clusters, &shared.ClusterReplicationConfiguration{
			ClusterName: common.StringPtr(replicationConfig.ClusterName),
		})
	}

	retention := int32(1)
	err := s.handler.registerDomain(context.Background(), &shared.RegisterDomainRequest{
		Name:                                   common.StringPtr(domainName),
		IsGlobalDomain:                         common.BoolPtr(isGlobalDomain),
		WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(retention),
	})
	s.Nil(err)

	resp, err := s.handler.describeDomain(context.Background(), &shared.DescribeDomainRequest{
		Name: common.StringPtr(domainName),
	})
	s.Nil(err)

	s.NotEmpty(resp.DomainInfo.GetUUID())
	resp.DomainInfo.UUID = common.StringPtr("")
	s.Equal(&shared.DomainInfo{
		Name:        common.StringPtr(domainName),
		Status:      shared.DomainStatusRegistered.Ptr(),
		Description: common.StringPtr(""),
		OwnerEmail:  common.StringPtr(""),
		Data:        map[string]string{},
		UUID:        common.StringPtr(""),
	}, resp.DomainInfo)
	s.Equal(&shared.DomainConfiguration{
		WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(retention),
		EmitMetric:                             common.BoolPtr(false),
		HistoryArchivalStatus:                  shared.ArchivalStatusDisabled.Ptr(),
		HistoryArchivalURI:                     common.StringPtr(""),
		VisibilityArchivalStatus:               shared.ArchivalStatusDisabled.Ptr(),
		VisibilityArchivalURI:                  common.StringPtr(""),
		BadBinaries:                            &shared.BadBinaries{Binaries: map[string]*shared.BadBinaryInfo{}},
	}, resp.Configuration)
	s.Equal(&shared.DomainReplicationConfiguration{
		ActiveClusterName: common.StringPtr(s.ClusterMetadata.GetCurrentClusterName()),
		Clusters:          clusters,
	}, resp.ReplicationConfiguration)
	s.Equal(common.EmptyVersion, resp.GetFailoverVersion())
	s.Equal(isGlobalDomain, resp.GetIsGlobalDomain())
}

func (s *domainHandlerGlobalDomainEnabledMasterClusterSuite) TestRegisterGetDomain_LocalDomain_NoDefault() {
	domainName := s.getRandomDomainName()
	description := "some random description"
	email := "some random email"
	retention := int32(7)
	emitMetric := true
	activeClusterName := cluster.TestCurrentClusterName
	clusters := []*shared.ClusterReplicationConfiguration{
		&shared.ClusterReplicationConfiguration{
			ClusterName: common.StringPtr(activeClusterName),
		},
	}
	data := map[string]string{"some random key": "some random value"}
	isGlobalDomain := false

	var expectedClusters []*shared.ClusterReplicationConfiguration
	for _, replicationConfig := range persistence.GetOrUseDefaultClusters(s.ClusterMetadata.GetCurrentClusterName(), nil) {
		expectedClusters = append(expectedClusters, &shared.ClusterReplicationConfiguration{
			ClusterName: common.StringPtr(replicationConfig.ClusterName),
		})
	}

	err := s.handler.registerDomain(context.Background(), &shared.RegisterDomainRequest{
		Name:                                   common.StringPtr(domainName),
		Description:                            common.StringPtr(description),
		OwnerEmail:                             common.StringPtr(email),
		WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(retention),
		EmitMetric:                             common.BoolPtr(emitMetric),
		Clusters:                               clusters,
		ActiveClusterName:                      common.StringPtr(activeClusterName),
		Data:                                   data,
		IsGlobalDomain:                         common.BoolPtr(isGlobalDomain),
	})
	s.Nil(err)

	resp, err := s.handler.describeDomain(context.Background(), &shared.DescribeDomainRequest{
		Name: common.StringPtr(domainName),
	})
	s.Nil(err)

	s.NotEmpty(resp.DomainInfo.GetUUID())
	resp.DomainInfo.UUID = common.StringPtr("")
	s.Equal(&shared.DomainInfo{
		Name:        common.StringPtr(domainName),
		Status:      shared.DomainStatusRegistered.Ptr(),
		Description: common.StringPtr(description),
		OwnerEmail:  common.StringPtr(email),
		Data:        data,
		UUID:        common.StringPtr(""),
	}, resp.DomainInfo)
	s.Equal(&shared.DomainConfiguration{
		WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(retention),
		EmitMetric:                             common.BoolPtr(emitMetric),
		HistoryArchivalStatus:                  shared.ArchivalStatusDisabled.Ptr(),
		HistoryArchivalURI:                     common.StringPtr(""),
		VisibilityArchivalStatus:               shared.ArchivalStatusDisabled.Ptr(),
		VisibilityArchivalURI:                  common.StringPtr(""),
		BadBinaries:                            &shared.BadBinaries{Binaries: map[string]*shared.BadBinaryInfo{}},
	}, resp.Configuration)
	s.Equal(&shared.DomainReplicationConfiguration{
		ActiveClusterName: common.StringPtr(s.ClusterMetadata.GetCurrentClusterName()),
		Clusters:          expectedClusters,
	}, resp.ReplicationConfiguration)
	s.Equal(common.EmptyVersion, resp.GetFailoverVersion())
	s.Equal(isGlobalDomain, resp.GetIsGlobalDomain())
}

func (s *domainHandlerGlobalDomainEnabledMasterClusterSuite) TestUpdateGetDomain_LocalDomain_NoAttrSet() {
	domainName := s.getRandomDomainName()
	description := "some random description"
	email := "some random email"
	retention := int32(7)
	emitMetric := true
	data := map[string]string{"some random key": "some random value"}
	var clusters []*shared.ClusterReplicationConfiguration
	for _, replicationConfig := range persistence.GetOrUseDefaultClusters(s.ClusterMetadata.GetCurrentClusterName(), nil) {
		clusters = append(clusters, &shared.ClusterReplicationConfiguration{
			ClusterName: common.StringPtr(replicationConfig.ClusterName),
		})
	}
	isGlobalDomain := false

	err := s.handler.registerDomain(context.Background(), &shared.RegisterDomainRequest{
		Name:                                   common.StringPtr(domainName),
		Description:                            common.StringPtr(description),
		OwnerEmail:                             common.StringPtr(email),
		WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(retention),
		EmitMetric:                             common.BoolPtr(emitMetric),
		Clusters:                               clusters,
		ActiveClusterName:                      common.StringPtr(s.ClusterMetadata.GetCurrentClusterName()),
		Data:                                   data,
		IsGlobalDomain:                         common.BoolPtr(isGlobalDomain),
	})
	s.Nil(err)

	fnTest := func(info *shared.DomainInfo, config *shared.DomainConfiguration,
		replicationConfig *shared.DomainReplicationConfiguration, isGlobalDomain bool, failoverVersion int64) {
		s.NotEmpty(info.GetUUID())
		info.UUID = common.StringPtr("")
		s.Equal(&shared.DomainInfo{
			Name:        common.StringPtr(domainName),
			Status:      shared.DomainStatusRegistered.Ptr(),
			Description: common.StringPtr(description),
			OwnerEmail:  common.StringPtr(email),
			Data:        data,
			UUID:        common.StringPtr(""),
		}, info)
		s.Equal(&shared.DomainConfiguration{
			WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(retention),
			EmitMetric:                             common.BoolPtr(emitMetric),
			HistoryArchivalStatus:                  shared.ArchivalStatusDisabled.Ptr(),
			HistoryArchivalURI:                     common.StringPtr(""),
			VisibilityArchivalStatus:               shared.ArchivalStatusDisabled.Ptr(),
			VisibilityArchivalURI:                  common.StringPtr(""),
			BadBinaries:                            &shared.BadBinaries{Binaries: map[string]*shared.BadBinaryInfo{}},
		}, config)
		s.Equal(&shared.DomainReplicationConfiguration{
			ActiveClusterName: common.StringPtr(s.ClusterMetadata.GetCurrentClusterName()),
			Clusters:          clusters,
		}, replicationConfig)
		s.Equal(common.EmptyVersion, failoverVersion)
		s.Equal(isGlobalDomain, isGlobalDomain)
	}

	updateResp, err := s.handler.updateDomain(context.Background(), &shared.UpdateDomainRequest{
		Name: common.StringPtr(domainName),
	})
	s.Nil(err)
	fnTest(updateResp.DomainInfo, updateResp.Configuration, updateResp.ReplicationConfiguration, updateResp.GetIsGlobalDomain(), updateResp.GetFailoverVersion())

	getResp, err := s.handler.describeDomain(context.Background(), &shared.DescribeDomainRequest{
		Name: common.StringPtr(domainName),
	})
	s.Nil(err)
	fnTest(getResp.DomainInfo, getResp.Configuration, getResp.ReplicationConfiguration, getResp.GetIsGlobalDomain(), getResp.GetFailoverVersion())
}

func (s *domainHandlerGlobalDomainEnabledMasterClusterSuite) TestUpdateGetDomain_LocalDomain_AllAttrSet() {
	domainName := s.getRandomDomainName()
	isGlobalDomain := false
	err := s.handler.registerDomain(context.Background(), &shared.RegisterDomainRequest{
		Name:                                   common.StringPtr(domainName),
		IsGlobalDomain:                         common.BoolPtr(isGlobalDomain),
		WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(1),
	})
	s.Nil(err)

	description := "some random description"
	email := "some random email"
	retention := int32(7)
	emitMetric := true
	data := map[string]string{"some random key": "some random value"}
	var clusters []*shared.ClusterReplicationConfiguration
	for _, replicationConfig := range persistence.GetOrUseDefaultClusters(s.ClusterMetadata.GetCurrentClusterName(), nil) {
		clusters = append(clusters, &shared.ClusterReplicationConfiguration{
			ClusterName: common.StringPtr(replicationConfig.ClusterName),
		})
	}

	fnTest := func(info *shared.DomainInfo, config *shared.DomainConfiguration,
		replicationConfig *shared.DomainReplicationConfiguration, isGlobalDomain bool, failoverVersion int64) {
		s.NotEmpty(info.GetUUID())
		info.UUID = common.StringPtr("")
		s.Equal(&shared.DomainInfo{
			Name:        common.StringPtr(domainName),
			Status:      shared.DomainStatusRegistered.Ptr(),
			Description: common.StringPtr(description),
			OwnerEmail:  common.StringPtr(email),
			Data:        data,
			UUID:        common.StringPtr(""),
		}, info)
		s.Equal(&shared.DomainConfiguration{
			WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(retention),
			EmitMetric:                             common.BoolPtr(emitMetric),
			HistoryArchivalStatus:                  shared.ArchivalStatusDisabled.Ptr(),
			HistoryArchivalURI:                     common.StringPtr(""),
			VisibilityArchivalStatus:               shared.ArchivalStatusDisabled.Ptr(),
			VisibilityArchivalURI:                  common.StringPtr(""),
			BadBinaries:                            &shared.BadBinaries{Binaries: map[string]*shared.BadBinaryInfo{}},
		}, config)
		s.Equal(&shared.DomainReplicationConfiguration{
			ActiveClusterName: common.StringPtr(s.ClusterMetadata.GetCurrentClusterName()),
			Clusters:          clusters,
		}, replicationConfig)
		s.Equal(common.EmptyVersion, failoverVersion)
		s.Equal(isGlobalDomain, isGlobalDomain)
	}

	updateResp, err := s.handler.updateDomain(context.Background(), &shared.UpdateDomainRequest{
		Name: common.StringPtr(domainName),
		UpdatedInfo: &shared.UpdateDomainInfo{
			Description: common.StringPtr(description),
			OwnerEmail:  common.StringPtr(email),
			Data:        data,
		},
		Configuration: &shared.DomainConfiguration{
			WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(retention),
			EmitMetric:                             common.BoolPtr(emitMetric),
			HistoryArchivalStatus:                  shared.ArchivalStatusDisabled.Ptr(),
			HistoryArchivalURI:                     common.StringPtr(""),
			VisibilityArchivalStatus:               shared.ArchivalStatusDisabled.Ptr(),
			VisibilityArchivalURI:                  common.StringPtr(""),
			BadBinaries:                            &shared.BadBinaries{Binaries: map[string]*shared.BadBinaryInfo{}},
		},
		ReplicationConfiguration: &shared.DomainReplicationConfiguration{
			ActiveClusterName: common.StringPtr(s.ClusterMetadata.GetCurrentClusterName()),
			Clusters:          clusters,
		},
	})
	s.Nil(err)
	fnTest(updateResp.DomainInfo, updateResp.Configuration, updateResp.ReplicationConfiguration, updateResp.GetIsGlobalDomain(), updateResp.GetFailoverVersion())

	getResp, err := s.handler.describeDomain(context.Background(), &shared.DescribeDomainRequest{
		Name: common.StringPtr(domainName),
	})
	s.Nil(err)
	fnTest(getResp.DomainInfo, getResp.Configuration, getResp.ReplicationConfiguration, getResp.GetIsGlobalDomain(), getResp.GetFailoverVersion())
}

func (s *domainHandlerGlobalDomainEnabledMasterClusterSuite) TestRegisterGetDomain_GlobalDomain_AllDefault() {
	domainName := s.getRandomDomainName()
	isGlobalDomain := true
	var clusters []*shared.ClusterReplicationConfiguration
	for _, replicationConfig := range persistence.GetOrUseDefaultClusters(s.ClusterMetadata.GetCurrentClusterName(), nil) {
		clusters = append(clusters, &shared.ClusterReplicationConfiguration{
			ClusterName: common.StringPtr(replicationConfig.ClusterName),
		})
	}

	s.mockProducer.On("Publish", mock.Anything).Return(nil).Once()

	retention := int32(1)
	err := s.handler.registerDomain(context.Background(), &shared.RegisterDomainRequest{
		Name:                                   common.StringPtr(domainName),
		IsGlobalDomain:                         common.BoolPtr(isGlobalDomain),
		WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(retention),
	})
	s.Nil(err)

	resp, err := s.handler.describeDomain(context.Background(), &shared.DescribeDomainRequest{
		Name: common.StringPtr(domainName),
	})
	s.Nil(err)

	s.NotEmpty(resp.DomainInfo.GetUUID())
	resp.DomainInfo.UUID = common.StringPtr("")
	s.Equal(&shared.DomainInfo{
		Name:        common.StringPtr(domainName),
		Status:      shared.DomainStatusRegistered.Ptr(),
		Description: common.StringPtr(""),
		OwnerEmail:  common.StringPtr(""),
		Data:        map[string]string{},
		UUID:        common.StringPtr(""),
	}, resp.DomainInfo)
	s.Equal(&shared.DomainConfiguration{
		WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(retention),
		EmitMetric:                             common.BoolPtr(false),
		HistoryArchivalStatus:                  shared.ArchivalStatusDisabled.Ptr(),
		HistoryArchivalURI:                     common.StringPtr(""),
		VisibilityArchivalStatus:               shared.ArchivalStatusDisabled.Ptr(),
		VisibilityArchivalURI:                  common.StringPtr(""),
		BadBinaries:                            &shared.BadBinaries{Binaries: map[string]*shared.BadBinaryInfo{}},
	}, resp.Configuration)
	s.Equal(&shared.DomainReplicationConfiguration{
		ActiveClusterName: common.StringPtr(s.ClusterMetadata.GetCurrentClusterName()),
		Clusters:          clusters,
	}, resp.ReplicationConfiguration)
	s.Equal(s.ClusterMetadata.GetNextFailoverVersion(s.ClusterMetadata.GetCurrentClusterName(), 0), resp.GetFailoverVersion())
	s.Equal(isGlobalDomain, resp.GetIsGlobalDomain())
}

func (s *domainHandlerGlobalDomainEnabledMasterClusterSuite) TestRegisterGetDomain_GlobalDomain_NoDefault() {
	domainName := s.getRandomDomainName()
	description := "some random description"
	email := "some random email"
	retention := int32(7)
	emitMetric := true
	activeClusterName := ""
	clusters := []*shared.ClusterReplicationConfiguration{}
	for clusterName := range s.ClusterMetadata.GetAllClusterInfo() {
		if clusterName != s.ClusterMetadata.GetCurrentClusterName() {
			activeClusterName = clusterName
		}
		clusters = append(clusters, &shared.ClusterReplicationConfiguration{
			ClusterName: common.StringPtr(clusterName),
		})
	}
	s.True(len(activeClusterName) > 0)
	s.True(len(clusters) > 1)
	data := map[string]string{"some random key": "some random value"}
	isGlobalDomain := true

	s.mockProducer.On("Publish", mock.Anything).Return(nil).Once()

	err := s.handler.registerDomain(context.Background(), &shared.RegisterDomainRequest{
		Name:                                   common.StringPtr(domainName),
		Description:                            common.StringPtr(description),
		OwnerEmail:                             common.StringPtr(email),
		WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(retention),
		EmitMetric:                             common.BoolPtr(emitMetric),
		Clusters:                               clusters,
		ActiveClusterName:                      common.StringPtr(activeClusterName),
		Data:                                   data,
		IsGlobalDomain:                         common.BoolPtr(isGlobalDomain),
	})
	s.Nil(err)

	resp, err := s.handler.describeDomain(context.Background(), &shared.DescribeDomainRequest{
		Name: common.StringPtr(domainName),
	})
	s.Nil(err)

	s.NotEmpty(resp.DomainInfo.GetUUID())
	resp.DomainInfo.UUID = common.StringPtr("")
	s.Equal(&shared.DomainInfo{
		Name:        common.StringPtr(domainName),
		Status:      shared.DomainStatusRegistered.Ptr(),
		Description: common.StringPtr(description),
		OwnerEmail:  common.StringPtr(email),
		Data:        data,
		UUID:        common.StringPtr(""),
	}, resp.DomainInfo)
	s.Equal(&shared.DomainConfiguration{
		WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(retention),
		EmitMetric:                             common.BoolPtr(emitMetric),
		HistoryArchivalStatus:                  shared.ArchivalStatusDisabled.Ptr(),
		HistoryArchivalURI:                     common.StringPtr(""),
		VisibilityArchivalStatus:               shared.ArchivalStatusDisabled.Ptr(),
		VisibilityArchivalURI:                  common.StringPtr(""),
		BadBinaries:                            &shared.BadBinaries{Binaries: map[string]*shared.BadBinaryInfo{}},
	}, resp.Configuration)
	s.Equal(&shared.DomainReplicationConfiguration{
		ActiveClusterName: common.StringPtr(activeClusterName),
		Clusters:          clusters,
	}, resp.ReplicationConfiguration)
	s.Equal(s.ClusterMetadata.GetNextFailoverVersion(activeClusterName, 0), resp.GetFailoverVersion())
	s.Equal(isGlobalDomain, resp.GetIsGlobalDomain())
}

func (s *domainHandlerGlobalDomainEnabledMasterClusterSuite) TestUpdateGetDomain_GlobalDomain_NoAttrSet() {
	domainName := s.getRandomDomainName()
	description := "some random description"
	email := "some random email"
	retention := int32(7)
	emitMetric := true
	data := map[string]string{"some random key": "some random value"}
	activeClusterName := ""
	clusters := []*shared.ClusterReplicationConfiguration{}
	for clusterName := range s.ClusterMetadata.GetAllClusterInfo() {
		if clusterName != s.ClusterMetadata.GetCurrentClusterName() {
			activeClusterName = clusterName
		}
		clusters = append(clusters, &shared.ClusterReplicationConfiguration{
			ClusterName: common.StringPtr(clusterName),
		})
	}
	s.True(len(activeClusterName) > 0)
	s.True(len(clusters) > 1)
	isGlobalDomain := true

	s.mockProducer.On("Publish", mock.Anything).Return(nil).Twice()

	err := s.handler.registerDomain(context.Background(), &shared.RegisterDomainRequest{
		Name:                                   common.StringPtr(domainName),
		Description:                            common.StringPtr(description),
		OwnerEmail:                             common.StringPtr(email),
		WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(retention),
		EmitMetric:                             common.BoolPtr(emitMetric),
		Clusters:                               clusters,
		ActiveClusterName:                      common.StringPtr(activeClusterName),
		Data:                                   data,
		IsGlobalDomain:                         common.BoolPtr(isGlobalDomain),
	})
	s.Nil(err)

	fnTest := func(info *shared.DomainInfo, config *shared.DomainConfiguration,
		replicationConfig *shared.DomainReplicationConfiguration, isGlobalDomain bool, failoverVersion int64) {
		s.NotEmpty(info.GetUUID())
		info.UUID = common.StringPtr("")
		s.Equal(&shared.DomainInfo{
			Name:        common.StringPtr(domainName),
			Status:      shared.DomainStatusRegistered.Ptr(),
			Description: common.StringPtr(description),
			OwnerEmail:  common.StringPtr(email),
			Data:        data,
			UUID:        common.StringPtr(""),
		}, info)
		s.Equal(&shared.DomainConfiguration{
			WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(retention),
			EmitMetric:                             common.BoolPtr(emitMetric),
			HistoryArchivalStatus:                  shared.ArchivalStatusDisabled.Ptr(),
			HistoryArchivalURI:                     common.StringPtr(""),
			VisibilityArchivalStatus:               shared.ArchivalStatusDisabled.Ptr(),
			VisibilityArchivalURI:                  common.StringPtr(""),
			BadBinaries:                            &shared.BadBinaries{Binaries: map[string]*shared.BadBinaryInfo{}},
		}, config)
		s.Equal(&shared.DomainReplicationConfiguration{
			ActiveClusterName: common.StringPtr(activeClusterName),
			Clusters:          clusters,
		}, replicationConfig)
		s.Equal(s.ClusterMetadata.GetNextFailoverVersion(activeClusterName, 0), failoverVersion)
		s.Equal(isGlobalDomain, isGlobalDomain)
	}

	updateResp, err := s.handler.updateDomain(context.Background(), &shared.UpdateDomainRequest{
		Name: common.StringPtr(domainName),
	})
	s.Nil(err)
	fnTest(updateResp.DomainInfo, updateResp.Configuration, updateResp.ReplicationConfiguration, updateResp.GetIsGlobalDomain(), updateResp.GetFailoverVersion())

	getResp, err := s.handler.describeDomain(context.Background(), &shared.DescribeDomainRequest{
		Name: common.StringPtr(domainName),
	})
	s.Nil(err)
	fnTest(getResp.DomainInfo, getResp.Configuration, getResp.ReplicationConfiguration, getResp.GetIsGlobalDomain(), getResp.GetFailoverVersion())
}

func (s *domainHandlerGlobalDomainEnabledMasterClusterSuite) TestUpdateGetDomain_GlobalDomain_AllAttrSet() {
	domainName := s.getRandomDomainName()
	activeClusterName := ""
	clusters := []*shared.ClusterReplicationConfiguration{}
	for clusterName := range s.ClusterMetadata.GetAllClusterInfo() {
		if clusterName != s.ClusterMetadata.GetCurrentClusterName() {
			activeClusterName = clusterName
		}
		clusters = append(clusters, &shared.ClusterReplicationConfiguration{
			ClusterName: common.StringPtr(clusterName),
		})
	}
	s.True(len(activeClusterName) > 0)
	s.True(len(clusters) > 1)
	isGlobalDomain := true

	s.mockProducer.On("Publish", mock.Anything).Return(nil).Twice()

	err := s.handler.registerDomain(context.Background(), &shared.RegisterDomainRequest{
		Name:                                   common.StringPtr(domainName),
		IsGlobalDomain:                         common.BoolPtr(isGlobalDomain),
		Clusters:                               clusters,
		ActiveClusterName:                      common.StringPtr(activeClusterName),
		WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(1),
	})
	s.Nil(err)

	description := "some random description"
	email := "some random email"
	retention := int32(7)
	emitMetric := true
	data := map[string]string{"some random key": "some random value"}

	fnTest := func(info *shared.DomainInfo, config *shared.DomainConfiguration,
		replicationConfig *shared.DomainReplicationConfiguration, isGlobalDomain bool, failoverVersion int64) {
		s.NotEmpty(info.GetUUID())
		info.UUID = common.StringPtr("")
		s.Equal(&shared.DomainInfo{
			Name:        common.StringPtr(domainName),
			Status:      shared.DomainStatusRegistered.Ptr(),
			Description: common.StringPtr(description),
			OwnerEmail:  common.StringPtr(email),
			Data:        data,
			UUID:        common.StringPtr(""),
		}, info)
		s.Equal(&shared.DomainConfiguration{
			WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(retention),
			EmitMetric:                             common.BoolPtr(emitMetric),
			HistoryArchivalStatus:                  shared.ArchivalStatusDisabled.Ptr(),
			HistoryArchivalURI:                     common.StringPtr(""),
			VisibilityArchivalStatus:               shared.ArchivalStatusDisabled.Ptr(),
			VisibilityArchivalURI:                  common.StringPtr(""),
			BadBinaries:                            &shared.BadBinaries{Binaries: map[string]*shared.BadBinaryInfo{}},
		}, config)
		s.Equal(&shared.DomainReplicationConfiguration{
			ActiveClusterName: common.StringPtr(activeClusterName),
			Clusters:          clusters,
		}, replicationConfig)
		s.Equal(s.ClusterMetadata.GetNextFailoverVersion(activeClusterName, 0), failoverVersion)
		s.Equal(isGlobalDomain, isGlobalDomain)
	}

	updateResp, err := s.handler.updateDomain(context.Background(), &shared.UpdateDomainRequest{
		Name: common.StringPtr(domainName),
		UpdatedInfo: &shared.UpdateDomainInfo{
			Description: common.StringPtr(description),
			OwnerEmail:  common.StringPtr(email),
			Data:        data,
		},
		Configuration: &shared.DomainConfiguration{
			WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(retention),
			EmitMetric:                             common.BoolPtr(emitMetric),
			HistoryArchivalStatus:                  shared.ArchivalStatusDisabled.Ptr(),
			HistoryArchivalURI:                     common.StringPtr(""),
			VisibilityArchivalStatus:               shared.ArchivalStatusDisabled.Ptr(),
			VisibilityArchivalURI:                  common.StringPtr(""),
			BadBinaries:                            &shared.BadBinaries{Binaries: map[string]*shared.BadBinaryInfo{}},
		},
		ReplicationConfiguration: &shared.DomainReplicationConfiguration{
			ActiveClusterName: nil,
			Clusters:          clusters,
		},
	})
	s.Nil(err)
	fnTest(updateResp.DomainInfo, updateResp.Configuration, updateResp.ReplicationConfiguration, updateResp.GetIsGlobalDomain(), updateResp.GetFailoverVersion())

	getResp, err := s.handler.describeDomain(context.Background(), &shared.DescribeDomainRequest{
		Name: common.StringPtr(domainName),
	})
	s.Nil(err)
	fnTest(getResp.DomainInfo, getResp.Configuration, getResp.ReplicationConfiguration, getResp.GetIsGlobalDomain(), getResp.GetFailoverVersion())
}

func (s *domainHandlerGlobalDomainEnabledMasterClusterSuite) TestUpdateGetDomain_GlobalDomain_Failover() {
	domainName := s.getRandomDomainName()
	description := "some random description"
	email := "some random email"
	retention := int32(7)
	emitMetric := true
	data := map[string]string{"some random key": "some random value"}
	prevActiveClusterName := ""
	nextActiveClusterName := s.ClusterMetadata.GetCurrentClusterName()
	clusters := []*shared.ClusterReplicationConfiguration{}
	for clusterName := range s.ClusterMetadata.GetAllClusterInfo() {
		if clusterName != nextActiveClusterName {
			prevActiveClusterName = clusterName
		}
		clusters = append(clusters, &shared.ClusterReplicationConfiguration{
			ClusterName: common.StringPtr(clusterName),
		})
	}
	s.True(len(prevActiveClusterName) > 0)
	s.True(len(clusters) > 1)
	isGlobalDomain := true

	s.mockProducer.On("Publish", mock.Anything).Return(nil).Twice()

	err := s.handler.registerDomain(context.Background(), &shared.RegisterDomainRequest{
		Name:                                   common.StringPtr(domainName),
		Description:                            common.StringPtr(description),
		OwnerEmail:                             common.StringPtr(email),
		WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(retention),
		EmitMetric:                             common.BoolPtr(emitMetric),
		Clusters:                               clusters,
		ActiveClusterName:                      common.StringPtr(prevActiveClusterName),
		Data:                                   data,
		IsGlobalDomain:                         common.BoolPtr(isGlobalDomain),
	})
	s.Nil(err)

	fnTest := func(info *shared.DomainInfo, config *shared.DomainConfiguration,
		replicationConfig *shared.DomainReplicationConfiguration, isGlobalDomain bool, failoverVersion int64) {
		s.NotEmpty(info.GetUUID())
		info.UUID = common.StringPtr("")
		s.Equal(&shared.DomainInfo{
			Name:        common.StringPtr(domainName),
			Status:      shared.DomainStatusRegistered.Ptr(),
			Description: common.StringPtr(description),
			OwnerEmail:  common.StringPtr(email),
			Data:        data,
			UUID:        common.StringPtr(""),
		}, info)
		s.Equal(&shared.DomainConfiguration{
			WorkflowExecutionRetentionPeriodInDays: common.Int32Ptr(retention),
			EmitMetric:                             common.BoolPtr(emitMetric),
			HistoryArchivalStatus:                  shared.ArchivalStatusDisabled.Ptr(),
			HistoryArchivalURI:                     common.StringPtr(""),
			VisibilityArchivalStatus:               shared.ArchivalStatusDisabled.Ptr(),
			VisibilityArchivalURI:                  common.StringPtr(""),
			BadBinaries:                            &shared.BadBinaries{Binaries: map[string]*shared.BadBinaryInfo{}},
		}, config)
		s.Equal(&shared.DomainReplicationConfiguration{
			ActiveClusterName: common.StringPtr(nextActiveClusterName),
			Clusters:          clusters,
		}, replicationConfig)
		s.Equal(s.ClusterMetadata.GetNextFailoverVersion(
			nextActiveClusterName,
			s.ClusterMetadata.GetNextFailoverVersion(prevActiveClusterName, 0),
		), failoverVersion)
		s.Equal(isGlobalDomain, isGlobalDomain)
	}

	updateResp, err := s.handler.updateDomain(context.Background(), &shared.UpdateDomainRequest{
		Name: common.StringPtr(domainName),
		ReplicationConfiguration: &shared.DomainReplicationConfiguration{
			ActiveClusterName: common.StringPtr(s.ClusterMetadata.GetCurrentClusterName()),
		},
	})
	s.Nil(err)
	fnTest(updateResp.DomainInfo, updateResp.Configuration, updateResp.ReplicationConfiguration, updateResp.GetIsGlobalDomain(), updateResp.GetFailoverVersion())

	getResp, err := s.handler.describeDomain(context.Background(), &shared.DescribeDomainRequest{
		Name: common.StringPtr(domainName),
	})
	s.Nil(err)
	fnTest(getResp.DomainInfo, getResp.Configuration, getResp.ReplicationConfiguration, getResp.GetIsGlobalDomain(), getResp.GetFailoverVersion())
}

func (s *domainHandlerGlobalDomainEnabledMasterClusterSuite) getRandomDomainName() string {
	return "domain" + uuid.New()
}
