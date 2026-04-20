import * as _ from 'lodash';
import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandParamsUtility, ProtectDeviceNameResponseCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the Protect Device Name Response Message command
 *
 * This message is issued by a controller or remote device in response to a PROTECT DEVICE NAME REQUEST message (Command Byte 17),
 * returning the device name protecting a particular destination.
 * When received by the controller the name is assumed to be an OEM device and when received by the remote device the name is assumed to be a panel name.
 *
 * Command issued by Pro-Bel Controller
 * @export
 * @class ProtectDeviceNameResponseCommand
 * @extends {CommandBase<ProtectDeviceNameResponseCommandParams>}
 */
export class ProtectDeviceNameResponseCommand extends CommandBase<ProtectDeviceNameResponseCommandParams, any> {
    /**
     * Creates an instance of ProtectDeviceNameResponseCommand.
     * @param {ProtectDeviceNameResponseCommandParams} params the command parameters
     * @memberof ProtectDeviceNameResponseCommand
     */
    constructor(params: ProtectDeviceNameResponseCommandParams) {
        super(CommandIdentifiers.TX.GENERAL.PROTECT_DEVICE_NAME_RESPONSE_MESSAGE, params);
        this.validateParams(params);
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof ProtectDeviceNameResponseCommand
     */
    toLogDescription(): string {
        return `General  -   ${this.name}: ${CommandParamsUtility.toString(this.params)}`;
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof ProtectDeviceNameResponseCommand
     */
    protected buildData(): Buffer {
        return this.buildDataNormal();
    }

    /**
     * Build the general command
     * Returns Probel SW-P-08 - Protect Device Name Response Message CMD_018_0X12
     *
     * + This message is issued by a controller or remote device in response to a PROTECT DEVICE NAME REQUEST message (Command Byte 17),
     * + returning the device name protecting a particular destination
     * + When received by the controller the name is assumed to be an OEM device and when received by the remote device the name is assumed to be a panel name.
     *
     *  | Message | Command Byte | 018 - 0x12                                                                                                                         |
     *  |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     *  | Byte    | Field Format | Notes                                                                                                                              |
     *  | Byte 1  | Multiplier   |                                                                                                                                    |
     *  |         | Bit[7]       | 0                                                                                                                                  |
     *  |         | Bits[4-6]    | 0                                                                                                                                  |
     *  |         | Bit[3]       | 0                                                                                                                                  |
     *  |         | Bits[0-2]    | Device number DIV 128 (0- 1023 Devices)                                                                                            |
     *  | Byte 2  | Device number| Device number MOD 128                                                                                                              |
     *  | Byte 3  | Name         | Eight character ASCII device name                                                                                                  |
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof ProtectDeviceNameResponseCommand
     */
    protected buildDataNormal(): Buffer {
        const buffer = new SmartBuffer({ size: 4 })
            .writeUInt8(this.id)
            .writeUInt8(BufferUtility.combine2BytesMultiplierMsbLsb(0, this.params.deviceId))
            .writeUInt8(this.params.deviceId % 128)
            // TODO: Padding character needs to be a settings managed by the admin system
            .writeString(_.padStart(this.params.deviceName, 8, ' '));
        return buffer.toBuffer();
    }

    /**
     * Validate the command parameters and throw a ValidationError in case of error
     * @private
     * @param {ProtectDeviceNameResponseCommandParams} params the command parameters
     * @memberof ProtectDeviceNameResponseCommand
     */
    private validateParams(params: ProtectDeviceNameResponseCommandParams): void {
        const validationErrors: Record<string, LocaleData> = new CommandParamsValidator(params).validate();
        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }
}
