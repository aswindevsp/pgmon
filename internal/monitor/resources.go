package monitor

import (
	"encoding/json"
	"time"
)

type ResourceList struct {
	Items []json.RawMessage `json:"items"`
}

type ClusterResource struct {
	Metadata struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
	} `json:"metadata"`
	Spec struct {
		Instances int `json:"instances"`
		Storage   struct {
			Size string `json:"size"`
		} `json:"storage"`
		Bootstrap struct {
			Recovery struct {
				Source   string `json:"source"`
				Database string `json:"database"`
			} `json:"recovery"`
		} `json:"bootstrap"`
		Backup *struct {
			BarmanObjectStore *struct {
				DestinationPath string `json:"destinationPath"`
			} `json:"barmanObjectStore"`
			VolumeSnapshot *struct {
				ClassName string `json:"className"`
			} `json:"volumeSnapshot"`
		} `json:"backup"`
		Plugins []struct {
			Name          string `json:"name"`
			IsWALArchiver *bool  `json:"isWALArchiver"`
		} `json:"plugins"`
	} `json:"spec"`
	Status struct {
		FirstRecoverabilityPoint string   `json:"firstRecoverabilityPoint"`
		Phase                    string   `json:"phase"`
		ReadyInstances           int      `json:"readyInstances"`
		Instances                int      `json:"instances"`
		CurrentPrimary           string   `json:"currentPrimary"`
		TargetPrimary            string   `json:"targetPrimary"`
		InitializingPVC          []string `json:"initializingPVC"`
		JobCount                 int      `json:"jobCount"`
		Image                    string   `json:"image"`
		Conditions               []struct {
			Type, Status, Message string
			LastTransitionTime    time.Time `json:"lastTransitionTime"`
		} `json:"conditions"`
		InstancesStatus map[string][]string `json:"instancesStatus"`
	} `json:"status"`
}

type PodResource struct {
	Metadata struct {
		Name              string            `json:"name"`
		CreationTimestamp time.Time         `json:"creationTimestamp"`
		Labels            map[string]string `json:"labels"`
		OwnerReferences   []struct {
			Kind string `json:"kind"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		NodeName   string `json:"nodeName"`
		Containers []struct {
			Name      string `json:"name"`
			Resources struct {
				Limits   map[string]string `json:"limits"`
				Requests map[string]string `json:"requests"`
			} `json:"resources"`
		} `json:"containers"`
	} `json:"spec"`
	Status struct {
		Phase             string `json:"phase"`
		ContainerStatuses []struct {
			Ready        bool `json:"ready"`
			RestartCount int  `json:"restartCount"`
		} `json:"containerStatuses"`
	} `json:"status"`
}

type NodeResource struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Status struct {
		Allocatable map[string]string `json:"allocatable"`
		Capacity    map[string]string `json:"capacity"`
	} `json:"status"`
}

type PVCResource struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Status struct {
		Capacity map[string]string `json:"capacity"`
		Phase    string            `json:"phase"`
	} `json:"status"`
}

type BackupResource struct {
	Metadata struct {
		Name              string    `json:"name"`
		UID               string    `json:"uid"`
		CreationTimestamp time.Time `json:"creationTimestamp"`
	} `json:"metadata"`
	Spec struct {
		Method  string `json:"method"`
		Cluster struct {
			Name string `json:"name"`
		} `json:"cluster"`
		PluginConfiguration struct {
			Name string `json:"name"`
		} `json:"pluginConfiguration"`
	} `json:"spec"`
	Status struct {
		Phase, Error, Method, BackupName, BackupID string
		BeginWal, EndWal, BeginLSN, EndLSN         string
		StartedAt                                  time.Time `json:"startedAt"`
		StoppedAt                                  time.Time `json:"stoppedAt"`
		ReconciliationStartedAt                    time.Time `json:"reconciliationStartedAt"`
		InstanceID                                 struct {
			PodName string `json:"podName"`
		} `json:"instanceID"`
	} `json:"status"`
}

type JobResource struct {
	Metadata struct {
		Name              string            `json:"name"`
		CreationTimestamp time.Time         `json:"creationTimestamp"`
		Labels            map[string]string `json:"labels"`
	} `json:"metadata"`
	Status struct {
		Active         int        `json:"active"`
		Succeeded      int        `json:"succeeded"`
		Failed         int        `json:"failed"`
		Ready          int        `json:"ready"`
		StartTime      *time.Time `json:"startTime"`
		CompletionTime *time.Time `json:"completionTime"`
	} `json:"status"`
}

type ScheduledBackupResource struct {
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Schedule string `json:"schedule"`
	} `json:"spec"`
	Status struct {
		LastScheduleTime *time.Time `json:"lastScheduleTime"`
	} `json:"status"`
}

type EventResource struct {
	Metadata struct {
		Name              string    `json:"name"`
		CreationTimestamp time.Time `json:"creationTimestamp"`
	} `json:"metadata"`
	InvolvedObject struct {
		Name string `json:"name"`
	} `json:"involvedObject"`
	Type, Reason, Message string
}
