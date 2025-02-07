package cluster

import (
	"fmt"
	"sync"

	g "github.com/onsi/ginkgo"

	reale2e "k8s.io/kubernetes/test/e2e"
	e2e "k8s.io/kubernetes/test/e2e/framework"

	exutil "github.com/openshift/origin/test/extended/util"
)

var viperConfig string

const (
	defaultThreadCount int = 10
	channelSize        int = 1024
)

var _ = g.Describe("[Feature:Performance][Serial][Slow] Load cluster", func() {
	defer g.GinkgoRecover()
	var oc = exutil.NewCLIWithoutNamespace("cl")

	g.BeforeEach(func() {
		var err error
		viperConfig = reale2e.GetViperConfig()
		e2e.Logf("Using config %v", viperConfig)
		err = ParseConfig(viperConfig, false)
		if err != nil {
			e2e.Failf("Error parsing config: %v", err)
		}
	})

	g.It("concurrently with templates", func() {
		project := ConfigContext.ClusterLoader.Projects
		if project == nil {
			e2e.Failf("Invalid config file.\nFile: %v", project)
		}

		numWorkers := ConfigContext.ClusterLoader.Threads
		if numWorkers == 0 {
			numWorkers = defaultThreadCount
		}

		work := make(chan ProjectMeta, channelSize)
		var wg sync.WaitGroup

		for i := 0; i < numWorkers; i++ {
			go clWorker(work, &wg, oc)
		}

		for _, p := range project {
			for j := 0; j < p.Number; j++ {
				wg.Add(1)
				unit := ProjectMeta{j, p, viperConfig}
				work <- unit
			}
		}

		wg.Wait()
		e2e.Logf("Closing channel")
		close(work)
	})
})

func clWorker(in <-chan ProjectMeta, wg *sync.WaitGroup, oc *exutil.CLI) {
	for {
		p, ok := <-in
		if !ok {
			e2e.Logf("Channel closed")
			return
		}
		var allArgs []string
		if p.NodeSelector != "" {
			allArgs = append(allArgs, "--node-selector")
			allArgs = append(allArgs, p.NodeSelector)
		}
		nsName := fmt.Sprintf("%s%d", p.Basename, p.Counter)
		allArgs = append(allArgs, nsName)

		// Check to see if the project exists
		projectExists, _ := ProjectExists(oc, nsName)
		if !projectExists {
			e2e.Logf("Project %s does not exist.", nsName)
		}

		// Based on configuration handle project existance
		switch p.IfExists {
		case IF_EXISTS_REUSE:
			e2e.Logf("Configuration requested reuse of project %v", nsName)
		case IF_EXISTS_DELETE:
			e2e.Logf("Configuration requested deletion of project %v", nsName)
			if projectExists {
				err := DeleteProject(oc, nsName, checkDeleteProjectInterval, checkDeleteProjectTimeout)
				e2e.Logf("Error deleting project: %v", err)
			}
		default:
			e2e.Failf("Unsupported ifexists value '%v' for project %v", p.IfExists, p)
		}

		if p.IfExists == IF_EXISTS_REUSE && projectExists {
			// do nothing
		} else {
			// Create namespaces as defined in Cluster Loader config
			err := oc.Run("adm", "new-project").Args(allArgs...).Execute()
			if err != nil {
				e2e.Logf("Error creating project: %v", err)

			} else {
				e2e.Logf("Created new namespace: %v", nsName)
			}
		}

		// Create templates as defined
		for _, template := range p.Templates {
			err := CreateSimpleTemplates(oc, nsName, p.ViperConfig, template)
			if err != nil {
				e2e.Logf("Error creating template: %v", err)
			}
		}

		wg.Done()
	}
}
