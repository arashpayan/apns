Introduction
------------
This Go package provides a client that can establish a connection with the Apple Push Notification service and deliver notifications to end users based on a device token. This package has been implemented according to Apple's documentation (available at ever changing/breaking URLs).

[![GoDoc](https://godoc.org/github.com/arashpayan/apns?status.png)](https://godoc.org/github.com/arashpayan/apns)

Installation
------------
`go get github.com/arashpayan/apns`

Usage
-----
### Create a Client and connect to the APNs

```go
client := apns.NewClient(apns.SandboxGateway, // or apns.ProductionGateway
    "/path/to/my/signed/cert/file.crt",
    "/path/to/my/private/key/file.key",
    func(invalidToken string) {
        // called when we try to send a notification to an invalid token
        // You should delete the token from your db by disassociating it with
        // the user you tried sending the notification too. When this is called
        // it means the person either uninstalled your app or has switched to
        // a new device.
    })
err := client.Connect()
if err != nil {
    // handle the connection error
}

```
### Create a Notification
```go
n := apns.NewNotification(aDeviceToken)
n.Alert = "Hello, user!"
n.Badge = 1
// optionally, include app data ('aps' element of notification json)
n.AppData = map[string]interface{}{"sound":"tada", "Foo":"Bar"}
```

### Send the Notification
```go
client.Send(n)
```
