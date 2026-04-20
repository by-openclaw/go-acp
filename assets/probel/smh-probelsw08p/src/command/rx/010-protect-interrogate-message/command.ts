import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifier, CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandParamsUtility, ProtectInterrogateMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the Protect Interrogate Message command
 *
 * This message is issued by the remote device to get the current protect status of a destination.
 * The controller will respond with a PROTECT TALLY message (Command Byte 11).
 *
 * Command issued by the remote device
 * @export
 * @class ProtectInterrogateMessageCommand
 * @extends {CommandBase<ProtectInterrogateMessageCommandParams>}
 */
export class ProtectInterrogateMessageCommand extends CommandBase<ProtectInterrogateMessageCommandParams, {}> {
    /**
     * Creates an instance of ProtectInterrogateMessageCommand
     *
     * @param {ProtectInterrogateMessageCommandParams} params the command parameters
     * @memberof ProtectInterrogateMessageCommand
     */
    constructor(params: ProtectInterrogateMessageCommandParams) {
        super(ProtectInterrogateMessageCommand.getCommandId(params), params);
        this.validateParams(params);
    }

    /**
     * Gets a boolean indicating whether the command is "General" or "Extended"
     * + General  : false => matrixId and LevelId are 4 bits coded and the multiplier of (destinationId / 128) must be smaller than 7 (3 bits coded)
     * + Extended : true
     *
     * @private
     * @static
     * @param {ProtectInterrogateMessageCommandParams} params the command parameters
     * @returns {boolean} 'true' is the command is extended otherwise 'false'
     * @memberof ProtectInterrogateMessageCommand
     */
    private static isExtended(params: ProtectInterrogateMessageCommandParams): boolean {
        // General Command is 4 bits = 16 range [0-15]
        if (params.matrixId > 15) {
            return true;
        }
        // 4 bits = 16 range [0-15]
        if (params.levelId > 15) {
            return true;
        }
        // two bytes
        if (params.destinationId > 895) {
            return true;
        }
        return false;
    }

    /**
     * Gets the Command Identifier based on the state of isExtended
     *
     * @private
     * @static
     * @param {ProtectInterrogateMessageCommandParams} params the command parameters
     * @returns {number} the command identifier
     * @memberof ProtectInterrogateMessageCommand
     */
    private static getCommandId(params: ProtectInterrogateMessageCommandParams): CommandIdentifier {
        return ProtectInterrogateMessageCommand.isExtended(params)
            ? CommandIdentifiers.RX.EXTENDED.PROTECT_INTERROGATE_MESSAGE
            : CommandIdentifiers.RX.GENERAL.PROTECT_INTERROGATE_MESSAGE;
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof ProtectInterrogateMessageCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string, withDeviceId: boolean): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params, withDeviceId)}`;

        return ProtectInterrogateMessageCommand.isExtended(this.params)
            ? descriptionFor('Extended', false)
            : descriptionFor('General', true);
    }

    /**
     * Build the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof ProtectInterrogateMessageCommand
     */
    protected buildData(): Buffer {
        return ProtectInterrogateMessageCommand.isExtended(this.params)
            ? this.buildDataExtended()
            : this.buildDataNormal();
    }

    /**
     * Validates the command parameters and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {ProtectInterrogateMessageCommandParams} params the command parameters
     * @memberof ProtectInterrogateMessageCommand
     */
    private validateParams(params: ProtectInterrogateMessageCommandParams): void {
        const validationErrors: Record<string, LocaleData> = new CommandParamsValidator(params).validate();

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }

    /**
     * Builds the normal command
     * Returns Probel SW-P-08 - General Protect Interrogate Message CMD_010_0X0A
     *
     * + This message is issued by the remote device to get the current protect status of a destination.
     * + The controller will respond with a PROTECT TALLY message (Command Byte 11).
     *
     * | Message | Command Byte | 010 - 0x0a                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  |              | Matrix/Level number                                                                                                                |
     * |         | Bits[4-7]    | Matrix Number                                                                                                                      |
     * |         | Bits[0-3]    | Level Number                                                                                                                       |
     * | Byte 2  | Multiplier   |                                                                                                                                    |
     * |         | Bit[7]       | 0                                                                                                                                  |
     * |         | Bits[4-6]    | Dest number DIV 128                                                                                                                |
     * |         | Bit[3]       | 0                                                                                                                                  |
     * |         | Bits[0-2]    | Device  number DIV 128 (0- 1023 Devices)                                                                                           |
     * | Byte 3  | Dest number  | Destination number MOD 128                                                                                                         |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof ProtectInterrogateMessageCommand
     */
    private buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 4 })
            .writeUInt8(this.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(this.params.matrixId, this.params.levelId))
            .writeUInt8(BufferUtility.combine2BytesMultiplierMsbLsb(this.params.destinationId, this.params.deviceId))
            .writeUInt8(this.params.destinationId % 128)
            .toBuffer();
    }

    /**
     * Builds the extended command
     * Returns Probel SW-P-08 - Extended General Protect Interrogate Message CMD_138_0X8A
     *
     * + This message is issued by the remote device to get the current protect status of a destination.
     * + The controller will respond with an EXTENDED PROTECT TALLY message (Command Byte 139).
     *
     * | Message |  Command Byte   | 138 - 0x8a                                                                                                                      |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format    | Notes                                                                                                                           |
     * | Byte 1  | Matrix number   |                                                                                                                                 |
     * | Byte 2  | Level number    |                                                                                                                                 |
     * | Byte 3  | Dest multiplier | Destination number DIV 256                                                                                                      |
     * | Byte 4  | Dest number     | Destination number MOD 256                                                                                                      |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof ProtectInterrogateMessageCommand
     */
    private buildDataExtended(): Buffer {
        const buffer = new SmartBuffer({ size: 5 })
            .writeUInt8(this.id)
            .writeUInt8(this.params.matrixId)
            .writeUInt8(this.params.levelId)
            .writeUInt8(Math.floor(this.params.destinationId / 256))
            .writeUInt8(this.params.destinationId % 256);
        return buffer.toBuffer();
    }
}
