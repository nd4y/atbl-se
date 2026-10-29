using System;
using System.Collections.Generic;
using Windows.Devices.WiFiDirect;

namespace AutoblowLocal {
 public sealed class HotspotHost : IDisposable {
  private WiFiDirectAdvertisementPublisher publisher;
  private WiFiDirectConnectionListener listener;
  private readonly List<WiFiDirectDevice> peers = new List<WiFiDirectDevice>();
  private readonly List<object> pending = new List<object>();
  public string Status { get { return publisher == null ? "Created" : publisher.Status.ToString(); } }
  public string LastPeerError { get; private set; }
  public void Start(string ssid, string password) {
   publisher = new WiFiDirectAdvertisementPublisher();
   listener = new WiFiDirectConnectionListener();
   listener.ConnectionRequested += OnConnectionRequested;
   publisher.Advertisement.IsAutonomousGroupOwnerEnabled = true;
   publisher.Advertisement.LegacySettings.IsEnabled = true;
   publisher.Advertisement.LegacySettings.Ssid = ssid;
   publisher.Advertisement.LegacySettings.Passphrase.Password = password;
   publisher.Start();
  }
  private void OnConnectionRequested(WiFiDirectConnectionListener sender, WiFiDirectConnectionRequestedEventArgs args) {
   try {
    using(var request = args.GetConnectionRequest()) {
     var operation = WiFiDirectDevice.FromIdAsync(request.DeviceInformation.Id);
     lock(pending) pending.Add(operation);
     operation.Completed = (op, status) => {
      try {
       if(status == Windows.Foundation.AsyncStatus.Completed) {
        var device = op.GetResults();
        lock(peers) { if(publisher == null) device.Dispose(); else peers.Add(device); }
       } else { LastPeerError = status.ToString(); }
      } catch(Exception e) { LastPeerError = e.GetType().Name; }
      finally { lock(pending) pending.Remove(op); }
     };
    }
   } catch(Exception e) { LastPeerError = e.GetType().Name; }
  }
  public void Dispose() {
   if(listener != null) { listener.ConnectionRequested -= OnConnectionRequested; listener = null; }
   if(publisher != null) { if(publisher.Status == WiFiDirectAdvertisementPublisherStatus.Started) publisher.Stop(); publisher = null; }
   lock(peers) { foreach(var peer in peers) peer.Dispose(); peers.Clear(); }
  }
 }
}
