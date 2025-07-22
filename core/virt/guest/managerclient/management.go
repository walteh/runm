//go:build !windows

package managerclient

// type Client struct {
// 	guestManagmentService vmmv1.GuestManagementServiceClient
// }

// var _ vmmv1.GuestManagementServiceClient = &Client{}

// // Readiness implements runtime.GuestManagement.
// func (me *Client) GuestReadiness(ctx context.Context, req *vmmv1.GuestReadinessRequest) (*vmmv1.GuestReadinessResponse, error) {
// 	_, err := me.guestManagmentService.GuestReadiness(ctx, &vmmv1.GuestReadinessRequest{})
// 	if err != nil {
// 		return nil, errors.Errorf("failed to get readiness: %w", err)
// 	}
// 	return &vmmv1.GuestReadinessResponse{}, nil
// }

// // RunCommand implements runtime.GuestManagement.
// func (me *Client) GuestRunCommand(ctx context.Context, req *vmmv1.GuestRunCommandRequest) (*vmmv1.GuestRunCommandResponse, error) {
// 	cmdreq := &vmmv1.GuestRunCommandRequest{}

// 	stdin, err := io.ReadAll(cmd.Stdin)
// 	if err != nil {
// 		return nil, errors.Errorf("failed to read stdin: %w", err)
// 	}
// 	cmdreq.SetStdin(stdin)
// 	cmdreq.SetArgc(strconv.Itoa(len(cmd.Args)))
// 	cmdreq.SetArgv(cmd.Args)
// 	envVars := make(map[string]string)
// 	for _, env := range cmd.Env {
// 		parts := strings.SplitN(env, "=", 2)
// 		if len(parts) == 2 {
// 			envVars[parts[0]] = parts[1]
// 		}
// 	}
// 	cmdreq.SetEnvVars(envVars)
// 	cmdreq.SetCwd(cmd.Dir)
// 	if cmd.SysProcAttr != nil {
// 		cmdreq.SetChroot(cmd.SysProcAttr.Chroot)
// 	}
// 	_, err = me.guestManagmentService.GuestRunCommand(ctx, cmdreq)
// 	if err != nil {
// 		return nil, errors.Errorf("failed to run command: %w", err)
// 	}
// 	return &vmmv1.GuestRunCommandResponse{}, nil
// }

// // TimeSync implements runtime.GuestManagement.
// func (me *Client) GuestTimeSync(ctx context.Context, req *vmmv1.GuestTimeSyncRequest) (*vmmv1.GuestTimeSyncResponse, error) {
// 	tsreq := &vmmv1.GuestTimeSyncRequest{}
// 	tsreq.SetUnixTimeNs(unixTimeNs)
// 	tsreq.SetTimezone(timezone)
// 	_, err := me.guestManagmentService.GuestTimeSync(ctx, tsreq)
// 	if err != nil {
// 		return nil, errors.Errorf("failed to time sync: %w", err)
// 	}
// 	return &vmmv1.GuestTimeSyncResponse{}, nil
// }
