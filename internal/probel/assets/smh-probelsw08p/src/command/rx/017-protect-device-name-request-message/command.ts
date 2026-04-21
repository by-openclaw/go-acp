import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandParamsUtility, ProtectDeviceNameRequestMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the Protect Device Name Reques tMessage command
 *
 * This message is issued by the remote device or controller when the device name protecting a particular destination is required.
 * When issued by a controller an OEM device name is being requested, when issued by the OEM remote device a Pro-Bel panel name is being requested.
 * The remote device or controller will respond with a PROTECT DEVICE NAME RESPONSE message (Command Byte 18).
 *
 * Command issued by the remote device
 *
 * @export
 * @class ProtectDeviceNAmeRequestMessageCommand
 * @extends {CommandBase<ProtectDeviceNameRequestMessageCommandParams>}
 */
export class ProtectDeviceNameRequestMessageCommand extends CommandBase<
    ProtectDeviceNameRequestMessageCommandParams,
    any
> {
    /**
     * Creates an instance of ProtectDeviceNameRequestMessageCommand
     *
     * @param {ProtectDeviceNameRequestMessageCommandParams} params the command parameters
     * @memberof ProtectDeviceNameRequestMessageCommand
     */
    constructor(params: ProtectDeviceNameRequestMessageCommandParams) {
        super(CommandIdentifiers.RX.GENERAL.PROTECT_DEVICE_NAME_REQUEST_MESSAGE, params);
        this.validateParams(params);
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof ProtectDeviceNAmeRequestMessageCommand
     */
    toLogDescription(): string {
        return `General - ${this.name}: ${CommandParamsUtility.toString(this.params)}`;
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof ProtectDeviceNAmeRequestMessageCommand
     */
    protected buildData(): Buffer {
        return this.buildDataNormal();
    }

    /**
     * Build the command
     * Returns Probel SW-P-08 - General Protect Device Name Request Message CMD_017_0X11
     *
     * + This message is issued by the remote device or controller when the device name protecting a particular destination is required.  When issued by a controller an OEM device name is being requested,
     * + when issued by the OEM remote device a Pro-Bel panel name is being requested.
     * + The remote device or controller will respond with a PROTECT DEVICE NAME RESPONSE message (Command Byte 18).
     *
     * | Message | Command Byte | 017 - 0x11                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 2  | Multiplier   |                                                                                                                                    |
     * |         | Bit[7]       | 0                                                                                                                                  |
     * |         | Bits[4-6]    | 0                                                                                                                                  |
     * |         | Bits[0-3]    | Device  number DIV 128 (0- 1023 Devices)                                                                                           |
     * | Byte 3  | Dest number  | Destination number MOD 128                                                                                                         |
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof ProtectDeviceNAmeRequestMessageCommand
     */
    protected buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 3 })
            .writeUInt8(this.id)
            .writeUInt8(BufferUtility.combine2BytesMultiplierMsbLsb(0, this.params.deviceId))
            .writeUInt8(this.params.deviceId % 128)
            .toBuffer();
    }

    /**
     * Validates the command parameters and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {ProtectDeviceNameRequestMessageCommandParams} params the command parameters
     * @memberof ProtectDeviceNameRequestMessageCommand
     */
    private validateParams(params: ProtectDeviceNameRequestMessageCommandParams): void {
        const validationErrors: Record<string, LocaleData> = new CommandParamsValidator(params).validate();

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }
}
