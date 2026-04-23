import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifier, CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandParamsUtility, ProtectConnectMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the Protect Connect Message command
 *
 * This is issued by the remote device to protect a destination.
 * The controller will respond with a PROTECT CONNECTED message (Command Byte 13).
 *
 * Command issued by the remote device
 *
 * @export
 * @class ProtectConnectMessageCommand
 * @extends {CommandBase<ProtectConnectMessageCommandParams>}
 */
export class ProtectConnectMessageCommand extends CommandBase<ProtectConnectMessageCommandParams, any> {
    /**
     * Creates an instance of ProtectConnectMessageCommand
     *
     * @param {ProtectConnectMessageCommandParams} params the command parameters
     * @memberof ProtectConnectMessageCommand
     */
    constructor(params: ProtectConnectMessageCommandParams) {
        super(ProtectConnectMessageCommand.getCommandId(params), params);
        this.validateParams(params);
    }

    /**
     * Gets a boolean indicating whether the command is "General" or "Extended"
     * + General  : false => matrixId and LevelId are 4 bits coded and the multiplier of (destinationId / 128) must be smaller than 7 (3 bits coded)
     * + Extended : true
     * @private
     * @static
     * @param {ProtectConnectMessageCommandParams} params the command parameters
     * @returns {boolean} 'true' is the command is extended otherwise 'false'
     * @memberof ProtectConnectMessageCommand
     */
    private static isExtended(params: ProtectConnectMessageCommandParams): boolean {
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
     * @param {ProtectConnectMessageCommandParams} params the command parameters
     * @returns {number} the command identifier
     * @memberof ProtectConnectMessageCommand
     */
    private static getCommandId(params: ProtectConnectMessageCommandParams): CommandIdentifier {
        return ProtectConnectMessageCommand.isExtended(params)
            ? CommandIdentifiers.RX.EXTENDED.PROTECT_CONNECT_MESSAGE
            : CommandIdentifiers.RX.GENERAL.PROTECT_CONNECT_MESSAGE;
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof ProtectConnectMessageCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)}`;

        return ProtectConnectMessageCommand.isExtended(this.params)
            ? descriptionFor('Extended')
            : descriptionFor('General');
    }

    /**
     * Build the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof ProtectConnectMessageCommand
     */
    protected buildData(): Buffer {
        return ProtectConnectMessageCommand.isExtended(this.params) ? this.buildDataExtended() : this.buildDataNormal();
    }

    /**
     * Validates the command parameters and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {ProtectConnectMessageCommandParams} params the command parameters
     * @memberof ProtectConnectMessageCommand
     */
    private validateParams(params: ProtectConnectMessageCommandParams): void {
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
     * Returns Probel SW-P-08 - General Protect Connect Message CMD_012_0X0c
     *
     * + This is issued by the remote device to protect a destination.
     * + The controller will respond with a PROTECT CONNECTED message (Command Byte 13).
     *
     * | Message | Command Byte | 012 - 0x0c                                                                                                                         |
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
     * @memberof ProtectConnectMessageCommand
     */
    private buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 5 })
            .writeUInt8(this.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(this.params.matrixId, this.params.levelId))
            .writeUInt8(BufferUtility.combine2BytesMultiplierMsbLsb(this.params.destinationId, this.params.deviceId))
            .writeUInt8(this.params.destinationId % 128)
            .writeUInt8(this.params.deviceId % 128)
            .toBuffer();
    }

    /**
     * Builds the extended command
     * Returns Probel SW-P-08 - Extended General Protect Connect Message CMD_140_0X8c
     *
     * + This is issued by the remote device to protect a destination.
     * + The controller will respond with a an EXTENDED PROTECT CONNECTED message (Command Byte 141).
     *
     * | Message |  Command Byte   | 140 - 0x8c                                                                                                                      |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format    | Notes                                                                                                                           |
     * | Byte 1  | Matrix number   |                                                                                                                                 |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte 2  | Level number    |                                                                                                                                 |
     * | Byte 3  | Dest multiplier | Destination number DIV 256                                                                                                      |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte 4  | Dest number     | Destination number MOD 256                                                                                                      |
     * | Byte 5  | Device multiplier| Device number DIV 256                                                                                                          |
     * | Byte 6  | Device number   | Device number MOD 256                                                                                                           |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof ProtectConnectMessageCommand
     */
    private buildDataExtended(): Buffer {
        const buffer = new SmartBuffer({ size: 7 })
            .writeUInt8(this.id)
            .writeUInt8(this.params.matrixId)
            .writeUInt8(this.params.levelId)
            .writeUInt8(Math.floor(this.params.destinationId / 256))
            .writeUInt8(this.params.destinationId % 256)
            .writeUInt8(Math.floor(this.params.deviceId / 256))
            .writeUInt8(this.params.deviceId % 256);
        return buffer.toBuffer();
    }
}
