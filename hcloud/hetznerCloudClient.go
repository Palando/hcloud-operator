package hcloud

import (
	"bytes"
	"context"

	"github.com/go-logr/logr"
	"github.com/palando/hcloud-operator/api/v1alpha1"
	hcloudv1alpha1 "github.com/palando/hcloud-operator/api/v1alpha1"

	// "flag"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	hc "github.com/hetznercloud/hcloud-go/hcloud"
	"golang.org/x/crypto/ssh"
)

const defaultLocation = "nbg1"

func NewHetznerCloudClient(token string, location hcloudv1alpha1.Location) (a *hc.Client, err error) {
	if location == "" {
		location = "nbg1"
	}

	var client = hc.NewClient(hc.WithToken(token))
	return client, nil
}

func GetVirtualMachineInfo(ctx context.Context, hclient hc.Client, vmName string, log logr.Logger) (a *hc.Server, err error) {
	vm, _, err := hclient.Server.Get(ctx, vmName)
	if err != nil {
		log.Error(err, "can not fetch virtual machine info from Hetzner cloud")
		return nil, err
	}

	if vm == nil {
		vm = &hc.Server{}
	}

	return vm, nil
}

func doKeyscan(ip string) {
	//ssh-keyscan -t rsa github.com >> ~/.ssh/known_hosts;
	cmd := exec.Command("./editKnownHosts", ip)
	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		fmt.Println(fmt.Sprint(err) + ": " + stderr.String())
		return
	}
	fmt.Println("Result: " + out.String())
}

func raw_connect(host string, port string) {
	timeout := time.Second
	for {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), timeout)
		if err != nil {
		}
		if conn != nil {
			defer conn.Close()
			fmt.Println("Opened", net.JoinHostPort(host, port))
			break
		}
	}
}

func doconn(ip string) {
	user := "root"
	command := "cloud-init status"
	port := "22"
	//doKeyscan(ip)
	raw_connect(ip, port)

	// Create the Signer for this private key.
	/*hostKeyCallback, err := knownhosts.New("/home/kimi/.ssh/known_hosts")
	if err != nil {
		fmt.Println("could not create hostkeycallback function: ", err)
	}*/

	//key, err := ioutil.ReadFile("/home/kimi/.ssh/id_rsa")
	config := &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			// Add in password check here for moar security.
			publicKey("/home/kimi/.ssh/id_rsa"),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	// Connect to the remote server and perform the SSH handshake.
	client, err := ssh.Dial("tcp", ip+":"+port, config)
	if err != nil {
		fmt.Printf("unable to connect: %v", err)
	}
	defer client.Close()

	ss, err := client.NewSession()
	if err != nil {
		fmt.Println("unable to create SSH session: ", err)
	}
	defer ss.Close()
	// Creating the buffer which will hold the remotly executed command's output.
	var stdoutBuf bytes.Buffer
	ss.Stdout = &stdoutBuf
	ss.Run(command)
	for {
		ss, _ := client.NewSession()
		ss.Stdout = &stdoutBuf
		ss.Run(command)
		if strings.Contains(stdoutBuf.String(), "done") {
			fmt.Println("breaking loop...")
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Let's print out the result of command.
}

func publicKey(path string) ssh.AuthMethod {
	key, err := ioutil.ReadFile(path)

	//	key, err := ioutil.ReadFile("/home/kimi/.ssh/id_rsa")
	if err != nil {
		fmt.Printf("unable to read private key: %v", err)
	}
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		fmt.Println(err)
	}
	return ssh.PublicKeys(signer)
}

func CreateVm(ctx context.Context, hclient hc.Client, vmCr v1alpha1.VirtualMachine, keys []*hc.SSHKey, log logr.Logger) (*hc.ServerCreateResult, *hc.ServerCreateOpts, error) {

	log.Info("CreteVm start")

	serverType, err := GetServerTypeByName(ctx, hclient, vmCr.Spec.VirtualMachineTemplateId, log)
	if err != nil {
		return nil, nil, err
	}

	log.Info("Got server type " + serverType.Name)

	location, err := GetLocation(ctx, hclient, string(vmCr.Spec.Location), log)
	if err != nil {
		return nil, nil, err
	}

	log.Info("Got location " + location.Name)

	image, err := GetImage(ctx, hclient, vmCr.Spec.Image, log)
	if err != nil {
		return nil, nil, err
	}

	log.Info("Got image " + image.Name)

	var start bool = true
	var serverOpts = hc.ServerCreateOpts{
		Name:             vmCr.Spec.Id,
		ServerType:       serverType,
		Image:            image,
		SSHKeys:          keys,
		Location:         location,
		Datacenter:       nil,
		UserData:         "",
		StartAfterCreate: &start,
		Labels:           nil,
		Automount:        nil,
		Volumes:          nil,
		Networks:         nil,
		Firewalls:        nil,
		PlacementGroup:   nil,
		PublicNet:        nil,
	}
	err = serverOpts.Validate()
	if err != nil {
		log.Error(err, "hcloud server options validation error")
		return nil, nil, err
	}

	result, _, err := hclient.Server.Create(ctx, serverOpts)
	if err != nil {
		log.Error(err, "error creating virtual machine "+vmCr.Spec.Id)
		return nil, nil, err
	}

	log.Info("Server create called")

	return &result, &serverOpts, nil
}

func GetLocation(ctx context.Context, hclient hc.Client, locationName string, log logr.Logger) (*hc.Location, error) {
	if locationName == "" {
		locationName = defaultLocation
	}
	location, _, err := hclient.Location.Get(ctx, locationName)
	if err != nil {
		log.Error(err, "failure getting location "+locationName)
		return nil, err
	}
	if location == nil {
		errorMessage := "failure getting location " + locationName
		err := fmt.Errorf(errorMessage)
		log.Error(err, errorMessage)
		return nil, err
	}
	return location, nil
}

func GetSshKeys(ctx context.Context, hclient hc.Client, keyNames []string, log logr.Logger) ([]*hc.SSHKey, error) {
	var keys []*hc.SSHKey
	for _, s := range keyNames {
		key, _, err := hclient.SSHKey.Get(ctx, s)
		if err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, nil
}

func GetImage(ctx context.Context, hclient hc.Client, imageName string, log logr.Logger) (*hc.Image, error) {
	image, _, err := hclient.Image.Get(ctx, imageName)
	if err != nil {
		log.Error(err, "failure getting image "+imageName)
		return nil, err
	}
	if image == nil {
		errorMessage := "failure getting image" + imageName
		err := fmt.Errorf(errorMessage)
		log.Error(err, errorMessage)
		return nil, err
	}
	return image, nil
}

func DeleteVm(ctx context.Context, hclient *hc.Client, server *hc.Server, log logr.Logger) error {
	_, err := hclient.Server.Delete(ctx, server)
	if err != nil {
		return err
	}
	return nil
}

func GetServerTypeByName(ctx context.Context, hclient hc.Client, serverTypeName string, log logr.Logger) (*hc.ServerType, error) {
	serverType, _, err := hclient.ServerType.Get(ctx, serverTypeName)
	if err != nil {
		log.Error(err, "failure getting server type "+serverTypeName)
		return nil, err
	}
	if serverType == nil {
		errorMessage := "failure getting server type" + serverTypeName
		err := fmt.Errorf(errorMessage)
		log.Error(err, errorMessage)
		return nil, err
	}
	return serverType, nil
}

func createServer(name string, locationIDOrName string, serverTypeName string, imageNameOrID string, userdata string, publicKey string) {
	// unique pair of SSHkey and Server, thus new SSHKey for every server
	client := hc.NewClient(hc.WithToken(os.Getenv("API_TOKEN")))
	// setting up server options
	serverOpts := hc.ServerCreateOpts{Name: name}
	serverOpts.Location, _, _ = client.Location.Get(context.Background(), locationIDOrName)
	serverOpts.ServerType, _, _ = client.ServerType.Get(context.Background(), serverTypeName)
	serverOpts.Image, _, _ = client.Image.Get(context.Background(), imageNameOrID)
	if userdata != "" {
		serverOpts.UserData = userdata
	}
	// validation of server options
	err := serverOpts.Validate()
	if err != nil {
		fmt.Printf("Error during validation: %v\n", err)
		return
	}
	// if the other server options are correct, we create the publicKey
	createKey(name+"-Key", publicKey)
	publicKeySSH, _, _ := client.SSHKey.Get(context.Background(), name+"-Key")
	serverOpts.SSHKeys = append(serverOpts.SSHKeys, publicKeySSH)

	result, _, err := client.Server.Create(context.Background(), serverOpts)
	if err != nil {
		fmt.Printf("Error during creation: %v", err)
	}
	if false {
		fmt.Println(result)
	}
	for {
		server, _, _ := client.Server.Get(context.Background(), "abc")
		if server.Status == "running" {
			break
			//fmt.Println("breaking loop", server.Status)
		}
		time.Sleep(10 * time.Millisecond)
		//fmt.Println(server.PublicNet.IPv4)

	}
	server, _, _ := client.Server.Get(context.Background(), "abc")
	doconn(server.PublicNet.IPv4.IP.String())
}

func testCreationTime() {
	start := time.Now()
	cloudconfig, _ := ioutil.ReadFile("cloudinit.yaml")
	createServer("abc", "flk1", "cx11", "ubuntu-20.04", string(cloudconfig), "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAACAQC1ORh6h8PpZ57zzx0rYBS/WjRu7ObAws6dSN+xQ5zcC1VZo2H/yJdcuyUU8HObkRZHRBTaMEbh3W3nnWj1PggeO7BQxUsLhtuSneI8FvIodbmYsyvAigReyv5pxfj9N0o06oCvkDP/kFTgidcAt1kUvBcSQfT97KltGYo4i+zVt6U+YCaeHOZTz7R11tHaOeh7b7A4z2olwcrhrfzq+s55WumvH0sM+Ohfh6Xo0FYgoO/G4XCLeymdYPbAA1JU96qarHF0sFBTv0zdCNl/grK2im4D4giSCjsYdxU9xFYLgsj8QIBZeAvQ7RSZTtlgh1IKsBvuQHBTwOzlVsb3YzJFVOI053TnMinhrJjJCtIWJYpVCW6QNNkMnCtiU+SAD0PKdX0uFF4Gy5/9K2m4PfPgyvtrjusPEGgkt3+BeKgbZHhoX8efktVBaj/aph0PUum3VkSPfBbduISsypl2cXCIOeTshBg3zPQxptK9qepMF1DY8JkRgQNSjcjPWy0MrLlAaG/UiUvgeFXhr6Hi5paIZ9bzSv1V66MNHvlxW3HXj4LtQjbZnDFfLo/pK+fMjSwW4ZDewgvYPrevMFvxEansEPbAIPvd0SYCjbRyOdSRH7hNH1bOapxiSZTD1Ja1P4umbRe1RXyRBgx02T7sAKvqJkUqpkgwDbowi6TxdTEXuQ== kimi@kimiarch")
	elapsed := time.Since(start)
	fmt.Printf("Creation of Server took %s\n", elapsed)
	deleteServer("abc")

}

/*
Ausprobieren:
Server erstellen, userdata:
Cloudinit angucken
über cloudinit Programme erstellen
ubuntu 20.04
über userdaten Programme installieren
Ziel: Docker und ZSH installieren
überprüfen
Was ist schneller? Docker über cloudinit zsh oder Docker über snapshot
*/

func deleteServer(name string) {
	client := hc.NewClient(hc.WithToken(os.Getenv("API_TOKEN")))
	serverToBeDeleted, _, err := client.Server.Get(context.Background(), name)
	if err != nil {
		fmt.Println(err)
	}
	deleteKey(name + "-Key")
	client.Server.Delete(context.Background(), serverToBeDeleted)
}

func createKey(name, publicKey string) {
	labels := make(map[string]string)
	client := hc.NewClient(hc.WithToken(os.Getenv("API_TOKEN")))
	SSHKeyCreateOpts := hc.SSHKeyCreateOpts{Name: name, PublicKey: publicKey, Labels: labels}
	_, _, error := client.SSHKey.Create(context.Background(), SSHKeyCreateOpts) //create SSHKey
	if error != nil {
		fmt.Printf("Error while creating sshKey: %v", error) //Print out SSHKey
	}

}

func deleteKey(name string) {
	client := hc.NewClient(hc.WithToken(os.Getenv("API_TOKEN")))
	sshKeys, _, err := client.SSHKey.GetByName(context.Background(), name)
	switch {
	case sshKeys == nil:
		fmt.Printf("The key %v does not exist.\n", name)
	case err != nil:
		fmt.Printf("%v", err)
	default:
		_, err = client.SSHKey.Delete(context.Background(), sshKeys)
	}
}

// func main() {
// 	// flag things
// 	delKey := flag.Bool("deletesshkey", false, "delete a key")
// 	crtKey := flag.Bool("createsshkey", false, "create a key")
// 	serv := flag.Bool("createServer", false, "create server")
// 	del := flag.Bool("del", false, "delete server")
// 	flag.Parse()
// 	switch {
// 	case *delKey && !*crtKey:
// 		deleteKey(os.Args[2])
// 	case *crtKey && !*delKey:
// 		name := os.Args[2]
// 		PublicKey := os.Args[3]
// 		createKey(name, PublicKey)
// 	case *serv:
// 		for i := 0; i < 1; i++ {
// 			testCreationTime()
// 		}
// 	case *del:
// 		deleteServer("abc")
// 	default:
// 		fmt.Printf("Please enter the mode exactly once. You entered delete:%v create:%v\n", *crtKey, *delKey)
// 	}
// }
