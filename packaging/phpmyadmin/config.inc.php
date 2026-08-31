<?php
// SPDX-License-Identifier: AGPL-3.0-or-later
// Managed by Stackfort. Do not edit.

declare(strict_types=1);

$stackfortBlowfishSecret = @file_get_contents('/var/lib/stackfort-phpmyadmin/blowfish.key');
if (! is_string($stackfortBlowfishSecret) || strlen($stackfortBlowfishSecret) !== 32) {
    throw new RuntimeException('Stackfort phpMyAdmin session key is unavailable.');
}

$cfg['blowfish_secret'] = $stackfortBlowfishSecret;
$cfg['Servers'] = [];
$i = 1;
$cfg['Servers'][$i]['auth_type'] = 'signon';
$cfg['Servers'][$i]['host'] = 'localhost';
$cfg['Servers'][$i]['SignonScript'] = '/etc/stackfort/phpmyadmin/signon.php';
$cfg['Servers'][$i]['SignonURL'] = '/phpmyadmin/stackfort-launch.php';
$cfg['Servers'][$i]['LogoutURL'] = '/';
$cfg['Servers'][$i]['AllowRoot'] = false;
$cfg['Servers'][$i]['AllowNoPassword'] = false;
$cfg['Servers'][$i]['persistent'] = false;
$cfg['Servers'][$i]['hide_connection_errors'] = true;
$cfg['ServerDefault'] = 1;
$cfg['AllowArbitraryServer'] = false;
$cfg['ShowPhpInfo'] = false;
$cfg['ShowServerInfo'] = false;
$cfg['VersionCheck'] = false;
$cfg['SendErrorReports'] = 'never';
$cfg['TempDir'] = '/var/lib/stackfort-phpmyadmin/tmp';
$cfg['UploadDir'] = '';
$cfg['SaveDir'] = '';

unset($stackfortBlowfishSecret);
