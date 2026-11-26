// Stub for the box compile check only (see ../README.md). Not the Apple header.
// Every declaration below is copied, availability attributes included, from the macOS SDK
// headers ImageCaptureCore.framework/Headers/{ICDevice,ICCameraDevice,ICDeviceBrowser,
// ImageCapturePlatform}.h of MacOSX15.5.sdk and MacOSX26.5.sdk (identical there; mirror
// github.com/alexey-lysiuk/macos-sdk). MacOSX13.3/14.5 differ only in spelling: the two
// authorization statuses are methods instead of properties (also unavailable on macOS).
// Only what camera/icc_darwin.m can touch is declared, but nothing is relaxed: iOS-only API
// stays unavailable on macOS and the delegate methods the SDK requires stay required, so
// the -Werror compile in ci/macos/check_icc_objc.sh fails exactly where the real SDK does.
#pragma once
#import <Foundation/Foundation.h>
#include <os/availability.h>

// ImageCapturePlatform.h (external builds)
#define IMAGECAPTURE_EXTERN extern __attribute__((visibility("default")))
#define IC_AVAILABLE(args...) API_AVAILABLE(args)
#define IC_UNAVAILABLE(args...) API_UNAVAILABLE(args)
#define IC_DEPRECATED(args...) API_DEPRECATED(args)

NS_ASSUME_NONNULL_BEGIN
typedef struct CGImage *CGImageRef;

// ICDevice.h
typedef NS_ENUM(NSUInteger, ICDeviceTypeMask){
	ICDeviceTypeMaskCamera = 0x00000001,
	ICDeviceTypeMaskScanner = 0x00000002,
} IC_AVAILABLE(macos(10.4), ios(15.2));
typedef NS_ENUM(NSUInteger, ICDeviceLocationTypeMask){
	ICDeviceLocationTypeMaskLocal = 0x00000100,
	ICDeviceLocationTypeMaskShared = 0x00000200,
	ICDeviceLocationTypeMaskBonjour = 0x00000400,
	ICDeviceLocationTypeMaskBluetooth = 0x00000800,
	ICDeviceLocationTypeMaskRemote = 0x0000FE00,
} IC_AVAILABLE(macos(10.4), ios(15.2));
typedef NSString *ICDeviceCapability;
typedef NSString *ICDeviceStatus;
typedef NSString *ICSessionOptions IC_AVAILABLE(macos(10.15), ios(13.0));

@class ICDevice;
@class ICCameraDevice;
@class ICCameraItem;
@class ICDeviceBrowser;

@protocol ICDeviceDelegate <NSObject>
@required
- (void)device:(ICDevice *)device didCloseSessionWithError:(NSError *_Nullable)error IC_AVAILABLE(macos(10.4), ios(13.0));
- (void)didRemoveDevice:(ICDevice *)device IC_AVAILABLE(macos(10.4), ios(13.0));
- (void)device:(ICDevice *)device didOpenSessionWithError:(NSError *_Nullable)error IC_AVAILABLE(macos(10.4), ios(13.0));
@optional
- (void)deviceDidBecomeReady:(ICDevice *)device IC_AVAILABLE(macos(10.4), ios(13.0));
- (void)deviceDidChangeName:(ICDevice *)device IC_AVAILABLE(macos(10.4)) IC_UNAVAILABLE(ios);
- (void)device:(ICDevice *)device didReceiveStatusInformation:(NSDictionary<ICDeviceStatus, id> *)status IC_AVAILABLE(macos(10.4), ios(13.0));
- (void)device:(ICDevice *)device didEncounterError:(NSError *_Nullable)error IC_AVAILABLE(macos(10.4), ios(13.0));
- (void)device:(ICDevice *)device didEjectWithError:(NSError *_Nullable)error IC_AVAILABLE(macos(10.4), ios(13.0));
@end

IC_AVAILABLE(macos(10.4), ios(13.0))
@interface ICDevice : NSObject
@property(nonatomic, readwrite, assign, nullable) id<ICDeviceDelegate> delegate IC_AVAILABLE(macos(10.4), ios(13.0));
@property(nonatomic, readonly) NSArray<NSString *> *capabilities IC_AVAILABLE(macos(10.4), ios(13.0));
@property(nonatomic, readonly, copy, nullable) NSString *name IC_AVAILABLE(macos(10.4), ios(13.0));
@property(nonatomic, readonly, copy, nullable) NSString *UUIDString IC_AVAILABLE(macos(10.4), ios(13.0));
@property(nonatomic, readonly, nullable) NSString *serialNumberString IC_AVAILABLE(macos(10.4)) IC_UNAVAILABLE(ios);
@property(nonatomic, readonly) int usbProductID IC_AVAILABLE(macos(10.4), ios(13.0));
@property(nonatomic, readonly) int usbVendorID IC_AVAILABLE(macos(10.4), ios(13.0));
- (void)requestOpenSessionWithOptions:(NSDictionary<ICSessionOptions, id> *_Nullable)options
                           completion:(void (^)(NSError *_Nullable error))completion IC_AVAILABLE(macos(10.15), ios(13.0));
- (void)requestCloseSessionWithOptions:(NSDictionary<ICSessionOptions, id> *_Nullable)options
                            completion:(void (^)(NSError *_Nullable error))completion IC_AVAILABLE(macos(10.15), ios(13.0));
@end

// ICCameraItem.h (opaque here)
@interface ICCameraItem : NSObject
@end

// ICCameraDevice.h
IMAGECAPTURE_EXTERN ICDeviceCapability const ICCameraDeviceCanAcceptPTPCommands IC_AVAILABLE(macos(10.4), ios(13.0));

@protocol ICCameraDeviceDelegate <ICDeviceDelegate>
// No @required keyword in the SDK: protocol methods before @optional are required.
- (void)cameraDevice:(ICCameraDevice *)camera didAddItems:(NSArray<ICCameraItem *> *)items IC_AVAILABLE(macos(10.4), ios(13.0));
- (void)cameraDevice:(ICCameraDevice *)camera didRemoveItems:(NSArray<ICCameraItem *> *)items IC_AVAILABLE(macos(10.4), ios(13.0));
- (void)cameraDevice:(ICCameraDevice *)camera
    didReceiveThumbnail:(CGImageRef _Nullable)thumbnail
                forItem:(ICCameraItem *)item
                  error:(NSError *_Nullable)error IC_AVAILABLE(macos(10.15), ios(13.0));
- (void)cameraDevice:(ICCameraDevice *)camera
    didReceiveMetadata:(NSDictionary *_Nullable)metadata
               forItem:(ICCameraItem *)item
                 error:(NSError *_Nullable)error IC_AVAILABLE(macos(10.15), ios(13.0));
- (void)cameraDevice:(ICCameraDevice *)camera didRenameItems:(NSArray<ICCameraItem *> *)items IC_AVAILABLE(macos(10.4), ios(13.0));
- (void)cameraDeviceDidChangeCapability:(ICCameraDevice *)camera IC_AVAILABLE(macos(10.4), ios(13.0));
- (void)cameraDevice:(ICCameraDevice *)camera didReceivePTPEvent:(NSData *)eventData IC_AVAILABLE(macos(10.4), ios(13.0));
- (void)deviceDidBecomeReadyWithCompleteContentCatalog:(ICCameraDevice *)device IC_AVAILABLE(macos(10.4), ios(13.0));
- (void)cameraDeviceDidRemoveAccessRestriction:(ICDevice *)device;
- (void)cameraDeviceDidEnableAccessRestriction:(ICDevice *)device;
@optional
- (BOOL)cameraDevice:(ICCameraDevice *)cameraDevice shouldGetThumbnailOfItem:(ICCameraItem *)item IC_AVAILABLE(macos(10.4), ios(13.0));
- (BOOL)cameraDevice:(ICCameraDevice *)cameraDevice shouldGetMetadataOfItem:(ICCameraItem *)item IC_AVAILABLE(macos(10.4), ios(13.0));
- (void)cameraDevice:(ICCameraDevice *)camera didCompleteDeleteFilesWithError:(nullable NSError *)error IC_AVAILABLE(macos(10.4), ios(13.0));
@end

IC_AVAILABLE(macos(10.4), ios(13.0))
@interface ICCameraDevice : ICDevice
- (void)requestSendPTPCommand:(NSData *)ptpCommand
                      outData:(NSData *_Nullable)ptpData
                   completion:(void (^)(NSData *responseData, NSData *ptpResponseData, NSError *_Nullable error))completion
    IC_AVAILABLE(macos(10.15), ios(13.0));
@end

// ICDeviceBrowser.h
typedef NSString *ICAuthorizationStatus IC_AVAILABLE(macos(10.15), ios(13.0));
IMAGECAPTURE_EXTERN ICAuthorizationStatus const ICAuthorizationStatusNotDetermined IC_UNAVAILABLE(macos) IC_AVAILABLE(ios(14.0));
IMAGECAPTURE_EXTERN ICAuthorizationStatus const ICAuthorizationStatusRestricted IC_UNAVAILABLE(macos) IC_AVAILABLE(ios(14.0));
IMAGECAPTURE_EXTERN ICAuthorizationStatus const ICAuthorizationStatusDenied IC_UNAVAILABLE(macos) IC_AVAILABLE(ios(14.0));
IMAGECAPTURE_EXTERN ICAuthorizationStatus const ICAuthorizationStatusAuthorized IC_UNAVAILABLE(macos) IC_AVAILABLE(ios(14.0));

@protocol ICDeviceBrowserDelegate <NSObject>
@required
- (void)deviceBrowser:(ICDeviceBrowser *)browser didAddDevice:(ICDevice *)device moreComing:(BOOL)moreComing IC_AVAILABLE(macos(10.4), ios(13.0));
- (void)deviceBrowser:(ICDeviceBrowser *)browser didRemoveDevice:(ICDevice *)device moreGoing:(BOOL)moreGoing IC_AVAILABLE(macos(10.4), ios(13.0));
@optional
- (void)deviceBrowser:(ICDeviceBrowser *)browser deviceDidChangeName:(ICDevice *)device IC_AVAILABLE(macos(10.4), ios(13.0));
- (void)deviceBrowser:(ICDeviceBrowser *)browser requestsSelectDevice:(ICDevice *)device IC_AVAILABLE(macos(10.4)) IC_UNAVAILABLE(ios);
- (void)deviceBrowserDidEnumerateLocalDevices:(ICDeviceBrowser *)browser IC_AVAILABLE(macos(10.4)) IC_UNAVAILABLE(ios);
- (void)deviceBrowserWillSuspendOperations:(ICDeviceBrowser *)browser IC_UNAVAILABLE(macos) IC_AVAILABLE(ios(14.0));
- (void)deviceBrowserDidSuspendOperations:(ICDeviceBrowser *)browser IC_UNAVAILABLE(macos) IC_AVAILABLE(ios(14.0));
- (void)deviceBrowserDidCancelSuspendOperations:(ICDeviceBrowser *)browser IC_UNAVAILABLE(macos) IC_AVAILABLE(ios(14.0));
- (void)deviceBrowserDidResumeOperations:(ICDeviceBrowser *)browser IC_UNAVAILABLE(macos) IC_AVAILABLE(ios(14.0));
@end

IC_AVAILABLE(macos(10.4), ios(13.0))
@interface ICDeviceBrowser : NSObject
@property(assign, nullable) id<ICDeviceBrowserDelegate> delegate;
@property(readonly, getter=isBrowsing) BOOL browsing;
@property(readonly, getter=isSuspended) BOOL suspended IC_UNAVAILABLE(macos) IC_AVAILABLE(ios(14.0));
@property(readwrite) ICDeviceTypeMask browsedDeviceTypeMask IC_AVAILABLE(macos(10.4), ios(15.2));
@property(readonly, nullable) NSArray<ICDevice *> *devices IC_AVAILABLE(macos(10.4), ios(13.0));
- (instancetype)init IC_AVAILABLE(macos(10.4), ios(13.0));
- (void)start IC_AVAILABLE(macos(10.4), ios(13.0));
- (void)stop IC_AVAILABLE(macos(10.4), ios(13.0));
@property(nonatomic, readonly, copy) ICAuthorizationStatus _Nonnull contentsAuthorizationStatus IC_UNAVAILABLE(macos) IC_AVAILABLE(ios(14.0));
- (void)requestContentsAuthorizationWithCompletion:(void (^)(ICAuthorizationStatus status))completion IC_UNAVAILABLE(macos) IC_AVAILABLE(ios(14.0));
@property(nonatomic, readonly, copy) ICAuthorizationStatus _Nonnull controlAuthorizationStatus IC_UNAVAILABLE(macos) IC_AVAILABLE(ios(14.0));
- (void)requestControlAuthorizationWithCompletion:(void (^)(ICAuthorizationStatus status))completion IC_UNAVAILABLE(macos) IC_AVAILABLE(ios(14.0));
- (void)resetContentsAuthorizationWithCompletion:(void (^)(ICAuthorizationStatus status))completion IC_UNAVAILABLE(macos) IC_AVAILABLE(ios(15.0));
- (void)resetControlAuthorizationWithCompletion:(void (^)(ICAuthorizationStatus status))completion IC_UNAVAILABLE(macos) IC_AVAILABLE(ios(15.0));
@end
NS_ASSUME_NONNULL_END
