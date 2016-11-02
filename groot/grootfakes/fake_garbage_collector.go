// This file was generated by counterfeiter
package grootfakes

import (
	"sync"

	"code.cloudfoundry.org/grootfs/groot"
	"code.cloudfoundry.org/lager"
)

type FakeGarbageCollector struct {
	CollectStub        func(logger lager.Logger, keepImages []string) error
	collectMutex       sync.RWMutex
	collectArgsForCall []struct {
		logger     lager.Logger
		keepImages []string
	}
	collectReturns struct {
		result1 error
	}
	invocations      map[string][][]interface{}
	invocationsMutex sync.RWMutex
}

func (fake *FakeGarbageCollector) Collect(logger lager.Logger, keepImages []string) error {
	var keepImagesCopy []string
	if keepImages != nil {
		keepImagesCopy = make([]string, len(keepImages))
		copy(keepImagesCopy, keepImages)
	}
	fake.collectMutex.Lock()
	fake.collectArgsForCall = append(fake.collectArgsForCall, struct {
		logger     lager.Logger
		keepImages []string
	}{logger, keepImagesCopy})
	fake.recordInvocation("Collect", []interface{}{logger, keepImagesCopy})
	fake.collectMutex.Unlock()
	if fake.CollectStub != nil {
		return fake.CollectStub(logger, keepImages)
	} else {
		return fake.collectReturns.result1
	}
}

func (fake *FakeGarbageCollector) CollectCallCount() int {
	fake.collectMutex.RLock()
	defer fake.collectMutex.RUnlock()
	return len(fake.collectArgsForCall)
}

func (fake *FakeGarbageCollector) CollectArgsForCall(i int) (lager.Logger, []string) {
	fake.collectMutex.RLock()
	defer fake.collectMutex.RUnlock()
	return fake.collectArgsForCall[i].logger, fake.collectArgsForCall[i].keepImages
}

func (fake *FakeGarbageCollector) CollectReturns(result1 error) {
	fake.CollectStub = nil
	fake.collectReturns = struct {
		result1 error
	}{result1}
}

func (fake *FakeGarbageCollector) Invocations() map[string][][]interface{} {
	fake.invocationsMutex.RLock()
	defer fake.invocationsMutex.RUnlock()
	fake.collectMutex.RLock()
	defer fake.collectMutex.RUnlock()
	return fake.invocations
}

func (fake *FakeGarbageCollector) recordInvocation(key string, args []interface{}) {
	fake.invocationsMutex.Lock()
	defer fake.invocationsMutex.Unlock()
	if fake.invocations == nil {
		fake.invocations = map[string][][]interface{}{}
	}
	if fake.invocations[key] == nil {
		fake.invocations[key] = [][]interface{}{}
	}
	fake.invocations[key] = append(fake.invocations[key], args)
}

var _ groot.GarbageCollector = new(FakeGarbageCollector)