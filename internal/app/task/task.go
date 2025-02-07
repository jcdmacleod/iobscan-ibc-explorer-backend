package task

import (
	"fmt"
	"time"

	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/conf"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/monitor"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/repository/cache"
	"github.com/bianjieai/iobscan-ibc-explorer-backend/internal/app/utils"
	"github.com/robfig/cron/v3"
	"github.com/sirupsen/logrus"
)

// Task cron task
type Task interface {
	Name() string
	Cron() int // CronExpression
	Run() int
	//ExpireTime() time.Duration // redis expireTime
}

var (
	tasks    []Task
	taskConf conf.Task
)

func RegisterTasks(task ...Task) {
	tasks = append(tasks, task...)
}

// GetTasks get all the task
func GetTasks() []Task {
	return tasks
}

func LoadTaskConf(taskCfg conf.Task) {
	taskConf = taskCfg
}

func Start() {
	if len(GetTasks()) == 0 {
		return
	}

	_ibcChainConfigTask.Run() // run chain config task immediately
	for _, v := range GetTasks() {
		task := v
		RunOnce(task)
	}

	c := cron.New(cron.WithSeconds())
	if taskConf.CronJobRelayerAddr == "" {
		taskConf.CronJobRelayerAddr = ThreeHourCronJobTime
	}
	_, err := c.AddFunc(taskConf.CronJobRelayerAddr, checkAndUpdateRelayerSrcChainAddr)
	if err != nil {
		logrus.Fatal("cron job err", err)
	}
	c.Start()
}

func RunOnce(task Task) {
	redisLockExpireTime := time.Duration(RedisLockExpireTime) * time.Second
	if taskConf.RedisLockExpireTime > 0 {
		redisLockExpireTime = time.Duration(taskConf.RedisLockExpireTime) * time.Second
	}

	utils.RunTimer(task.Cron(), utils.Sec, func() {
		//lock redis mux
		lockKey := fmt.Sprintf("%s:%s", "task", task.Name())
		if err := cache.GetRedisClient().Lock(lockKey, time.Now().Unix(), redisLockExpireTime); err != nil {
			logrus.Errorf("redis lock failed, name:%s, err:%v", task.Name(), err.Error())
			return
		}
		startTime := time.Now().Unix()
		logrus.Infof("task %s start", task.Name())
		metricValue := task.Run()
		monitor.SetCronTaskStatusMetricValue(task.Name(), float64(metricValue))
		//unlock redis mux
		cache.GetRedisClient().Del(lockKey)
		logrus.Infof("task %s end, time use %d(s), exec status: %d", task.Name(), time.Now().Unix()-startTime, metricValue)
	})
}

// ============================================================================
// ============================================================================

// OneOffTask one-off task
type OneOffTask interface {
	Name() string
	Switch() bool
	Run() int
}

var oneOffTasks []OneOffTask

func RegisterOneOffTasks(task ...OneOffTask) {
	oneOffTasks = append(oneOffTasks, task...)
}

func StartOneOffTask() {
	if len(oneOffTasks) == 0 {
		return
	}

	_ibcChainConfigTask.Run() // run chain config task immediately
	for _, v := range oneOffTasks {
		task := v
		go OneOffTaskRun(task)
	}
}

func OneOffTaskRun(task OneOffTask) {
	if !task.Switch() {
		logrus.Infof("one-off task %s closed", task.Name())
		return
	}
	lockKey := fmt.Sprintf("%s:%s", "one_off_task", task.Name())
	if err := cache.GetRedisClient().Lock(lockKey, time.Now().Unix(), OneOffTaskLockTime*time.Second); err != nil {
		logrus.Errorf("one-off task %s has been executed, err:%v", task.Name(), err.Error())
		return
	}
	logrus.Infof("one-off task %s start", task.Name())
	startTime := time.Now().Unix()
	res := task.Run()

	if res != 1 { // 为避免错误操作、重启、扩容等因素带来的风险，one-ff task 执行成功时不释放锁
		_, _ = cache.GetRedisClient().Del(lockKey)
	}

	logrus.Infof("one-off task %s end, time use %d(s), exec status: %d", task.Name(), time.Now().Unix()-startTime, res)
}
