// Copyright 2014 Arash Payan. All rights reserved.
// Use of this source code is governed by the Apache 2
// license that can be found in the LICENSE file.

/*
Package apns provides a client for using the Apple Push Notification service.
To start using it, all you need is your signed APNs certificate from Apple, and
your private key.

Using

To interact with the APNs, you must create a Client and connect it to the appropriate gateway. While your developing your Go app, you probably want to use the SandboxGateway.

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

After successfully connecting to an APNs gateway, create a Notification:

    n := apns.NewNotification(aDeviceToken)
    n.Alert = "Hello, user!"
    n.Badge = 1
    // optionally, include app data ('aps' element of notification json)
    n.AppData = map[string]interface{}{"sound":"tada", "Foo":"Bar"}

Finally, send the Notification via the Client:

    client.Send(n)
*/
package apns
