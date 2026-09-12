// SPDX-License-Identifier: AGPL-3.0-or-later

package storageprep

// RenderRecoveryGRUBEntry keeps the normal menu/kernel/initrd intact but sets a
// temporary fail-closed default while custom.cfg exists. A missing/consumed/
// conflicting environment cannot select conversion automatically. Finalization
// retires this file only after successful filesystem verification.
func RenderRecoveryGRUBEntry(plan Plan) (string, error) {
	id, err := GRUBEntryID(plan)
	if err != nil {
		return "", err
	}
	recovery := id + "-recovery"
	condition := "[ \"$stackfort_native_armed\" = \"" + plan.OperationID + "\" -a \"$stackfort_native_consumed\" = \"\" ]"
	setup := "  insmod part_gpt\n  insmod ext2\n  search --no-floppy --fs-uuid --set=root " + plan.RootUUID + "\n"
	linux := "  linux /boot/vmlinuz-" + plan.Kernel + " root=PARTUUID=" + plan.PartitionUUID + " ro console=tty0 console=ttyS0,115200 consoleblank=0"
	image := "  initrd /boot/" + id + ".img\n"
	return "# Stackfort fail-closed temporary boot default\nset default='" + recovery + "'\nif " + condition + "; then\n  set default='" + id + "'\nfi\n" +
		"menuentry 'Stackfort native quota preparation' --id '" + id + "' {\n" + setup +
		"  if " + condition + "; then\n    set stackfort_native_armed=\n    set stackfort_native_consumed=" + plan.OperationID + "\n    save_env stackfort_native_armed stackfort_native_consumed\n" +
		linux + " stackfort.native-quota=" + plan.OperationID + "\n  else\n" + linux + " stackfort.native-recovery=" + plan.OperationID + "\n  fi\n" + image + "}\n" +
		"menuentry 'Stackfort interrupted conversion - recovery required' --id '" + recovery + "' {\n" + setup + linux + " stackfort.native-recovery=" + plan.OperationID + "\n" + image + "}\n", nil
}
