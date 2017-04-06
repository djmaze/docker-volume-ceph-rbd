package main

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/Sirupsen/logrus"
	"github.com/docker/go-plugins-helpers/volume"
)

const socketAddress = "/run/docker/plugins/ceph-rbd.sock"

type cephRbdVolume struct {
	Pool     string
	Rbd      string
	Hosts    string
	Username string
	Secret   string

	RbdNum      int
	Mountpoint  string
	connections int
}

type cephRbdDriver struct {
	sync.RWMutex

	root      string
	statePath string
	volumes   map[string]*cephRbdVolume
}

func newCephRbdDriver(root string) (*cephRbdDriver, error) {
	logrus.WithField("method", "new driver").Debug(root)

	d := &cephRbdDriver{
		root:      filepath.Join(root, "volumes"),
		statePath: filepath.Join(root, "state", "ceph-rbd-state.json"),
		volumes:   map[string]*cephRbdVolume{},
	}

	data, err := ioutil.ReadFile(d.statePath)
	if err != nil {
		if os.IsNotExist(err) {
			logrus.WithField("statePath", d.statePath).Debug("no state found")
		} else {
			return nil, err
		}
	} else {
		if err := json.Unmarshal(data, &d.volumes); err != nil {
			return nil, err
		}
	}

	return d, nil
}

func (d *cephRbdDriver) saveState() {
	data, err := json.Marshal(d.volumes)
	if err != nil {
		logrus.WithField("statePath", d.statePath).Error(err)
		return
	}

	if err := ioutil.WriteFile(d.statePath, data, 0644); err != nil {
		logrus.WithField("savestate", d.statePath).Error(err)
	}
}

func (d *cephRbdDriver) Create(r volume.Request) volume.Response {
	logrus.WithField("method", "create").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()
	v := &cephRbdVolume{}

	for key, val := range r.Options {
		switch key {
		case "pool":
			v.Pool = val
		case "rbd":
			v.Rbd = val
		case "hosts":
			v.Hosts = val
		case "username":
			v.Username = val
		case "secret":
			v.Secret = val
		default:
			return responseError(fmt.Sprintf("unknown option %q", val))
		}
	}

	if v.Pool == "" {
		return responseError("'pool' option required")
	}
	if v.Rbd == "" {
		return responseError("'rbd' option required")
	}
	if v.Hosts == "" {
		return responseError("'hosts' option required")
	}
	if v.Username == "" {
		return responseError("'username' option required")
	}
	if v.Secret == "" {
		return responseError("'secret' option required")
	}
	v.Mountpoint = filepath.Join(d.root, fmt.Sprintf("%x", md5.Sum([]byte(v.Rbd)))) // TODO Include pool, hosts

	d.volumes[r.Name] = v

	d.saveState()

	logrus.WithField("method", "create").Debugf("Saved mountpoint %s", v.Mountpoint)

	return volume.Response{}
}

func (d *cephRbdDriver) Remove(r volume.Request) volume.Response {
	logrus.WithField("method", "remove").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	if v.connections != 0 {
		return responseError(fmt.Sprintf("volume %s is currently used by a container", r.Name))
	}
	if err := os.RemoveAll(v.Mountpoint); err != nil {
		return responseError(err.Error())
	}
	delete(d.volumes, r.Name)
	d.saveState()
	return volume.Response{}
}

func (d *cephRbdDriver) Path(r volume.Request) volume.Response {
	logrus.WithField("method", "path").Debugf("%#v", r)

	d.RLock()
	defer d.RUnlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	return volume.Response{Mountpoint: v.Mountpoint}
}

func (d *cephRbdDriver) Mount(r volume.MountRequest) volume.Response {
	logrus.WithField("method", "mount").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	if v.connections == 0 {
		fi, err := os.Lstat(v.Mountpoint)
		if os.IsNotExist(err) {
			if err := os.MkdirAll(v.Mountpoint, 0755); err != nil {
				return responseError(err.Error())
			}
		} else if err != nil {
			return responseError(err.Error())
		}

		if fi != nil && !fi.IsDir() {
			return responseError(fmt.Sprintf("%v already exist and it's not a directory", v.Mountpoint))
		}

		if err := d.mountVolume(v); err != nil {
			return responseError(err.Error())
		}
	}

	v.connections++

	return volume.Response{Mountpoint: v.Mountpoint}
}

func (d *cephRbdDriver) Unmount(r volume.UnmountRequest) volume.Response {
	logrus.WithField("method", "unmount").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()
	v, ok := d.volumes[r.Name]
	if !ok {
		return responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	v.connections--

	if v.connections <= 0 {
		if err := d.unmountVolume(v); err != nil {
			return responseError(err.Error())
		}
		v.connections = 0
	}

	return volume.Response{}
}

func (d *cephRbdDriver) Get(r volume.Request) volume.Response {
	logrus.WithField("method", "get").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	v, ok := d.volumes[r.Name]
	if !ok {
		return responseError(fmt.Sprintf("volume %s not found", r.Name))
	}

	return volume.Response{Volume: &volume.Volume{Name: r.Name, Mountpoint: v.Mountpoint}}
}

func (d *cephRbdDriver) List(r volume.Request) volume.Response {
	logrus.WithField("method", "list").Debugf("%#v", r)

	d.Lock()
	defer d.Unlock()

	var vols []*volume.Volume
	for name, v := range d.volumes {
		vols = append(vols, &volume.Volume{Name: name, Mountpoint: v.Mountpoint})
	}
	return volume.Response{Volumes: vols}
}

func (d *cephRbdDriver) Capabilities(r volume.Request) volume.Response {
	logrus.WithField("method", "capabilities").Debugf("%#v", r)

	return volume.Response{Capabilities: volume.Capability{Scope: "local"}}
}

func (d *cephRbdDriver) mountVolume(v *cephRbdVolume) error {
	fileName := "/host/sys/bus/rbd/add"
	mountString := fmt.Sprintf("%s name=%s,secret=%s %s %s", v.Hosts, v.Username, v.Secret, v.Pool, v.Rbd)

	if err := ioutil.WriteFile(fileName, []byte(mountString), 0600); err != nil {
		logrus.WithField("mountvolume", v.Rbd).Error(err)
		return err
	}

	num, err := findRbdNum(v)
	if err != nil {
		return err
	}
	v.RbdNum = num

	cmd := exec.Command("mount", fmt.Sprintf("/dev/rbd%d", v.RbdNum), v.Mountpoint)
	logrus.Debug(cmd.Args)
	return cmd.Run()
}

func (d *cephRbdDriver) unmountVolume(v *cephRbdVolume) error {
	cmd := fmt.Sprintf("umount %s", v.Mountpoint)
	logrus.Debug(cmd)
	if err := exec.Command("sh", "-c", cmd).Run(); err != nil {
		return err
	}

	return ioutil.WriteFile("/host/sys/bus/rbd/remove", []byte(strconv.Itoa(v.RbdNum)), 0600)
}

func findRbdNum(v *cephRbdVolume) (int, error) {
	// FIXME Use pool for searching as well
	cmd := fmt.Sprintf("grep -l %s /host/sys/devices/rbd/*/name | egrep -o '[0-9]+' | head -n1", v.Rbd)
	logrus.Debug(cmd)
	output, err := exec.Command("sh", "-c", cmd).Output()
	logrus.Debug(output)
	if err != nil {
		return -1, err
	}

	num, err := strconv.Atoi(strings.TrimSpace(string(output)))
	if err != nil {
		return -1, err
	}

	return num, nil
}

func responseError(err string) volume.Response {
	logrus.Error(err)
	return volume.Response{Err: err}
}

func main() {
	debug := os.Getenv("DEBUG")
	if ok, _ := strconv.ParseBool(debug); ok {
		logrus.SetLevel(logrus.DebugLevel)
	}

	d, err := newCephRbdDriver("/mnt")
	if err != nil {
		log.Fatal(err)
	}
	h := volume.NewHandler(d)
	logrus.Infof("listening on %s", socketAddress)
	logrus.Error(h.ServeUnix(socketAddress, 0))
}
