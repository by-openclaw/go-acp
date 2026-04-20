import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifier, CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandParamsUtility, ProtectDisConnectMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the Protect Dis Connect Message command
 *
 * This message is issued by the remote device to remove protection from a destination.
 * The controller will issue a PROTECT DIS-CONNECTED message (Command Byte 15) in response.
 *
 * Command issued by the remote device
 *
 * @export
 * @class ProtectDisConnectMessageCommand
 * @extends {CommandBase<ProtectDisConnectMessageCommandParams>}
 */
export class ProtectDisConnectMessageCommand extends CommandBase<ProtectDisConnectMessageCommandParams, any> {
    /**
     * Creates an instance of ProtectDisConnectMessageCommand
     *
     * @param {ProtectDisConnectMessageCommandParams} params the command parameters
     * @memberof ProtectDisConnectMessageCommand
     */
    constructor(params: ProtectDisConnectMessageCommandParams) {
        super(ProtectDisConnectMessageCommand.getCommandId(params), params);
        this.validateParams(params);
    }

    /**
     * Gets a boolean indicating whether the command is "General" or "Extended"
     * + General  : false => matrixId and LevelId are 4 bits coded and the multiplier of (destinationId / 128) must be smaller than 7 (3 bits coded)
     * + Extended : true
     * @private
     * @static
     * @param {ProtectDisConnectMessageCommandParams} params the command parameters
     * @returns {boolean} 'true' is the command is extended otherwise 'false'
     * @memberof ProtectDisConnectMessageCommand
     */
    private static isExtended(params: ProtectDisConnectMessageCommandParams): boolean {
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
     * @param {ProtectDisConnectMessageCommandParams} params the command parameters
     * @returns {number} the command identifier
     * @memberof ProtectDisConnectMessageCommand
     */
    private static getCommandId(params: ProtectDisConnectMessageCommandParams): CommandIdentifier {
        return ProtectDisConnectMessageCommand.isExtended(params)
            ? CommandIdentifiers.RX.EXTENDED.PROTECT_DIS_CONNECT_MESSAGE
            : CommandIdentifiers.RX.GENERAL.PROTECT_DIS_CONNECT_MESSAGE;
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof ProtectDisConnectMessageCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)}`;

        return ProtectDisConnectMessageCommand.isExtended(this.params)
            ? descriptionFor('Extended')
            : descriptionFor('General');
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof ProtectDisConnectMessageCommand
     */
    protected buildData(): Buffer {
        return ProtectDisConnectMessageCommand.isExtended(this.params)
            ? this.buildDataExtended()
            : this.buildDataNormal();
    }

    /**
     * Validates the command parameters and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {ProtectDisConnectMessageCommandParams} params the command parameters
     * @memberof ProtectDisConnectMessageCommand
     */
    private validateParams(params: ProtectDisConnectMessageCommandParams): void {
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
     * Returns Probel SW-P-08 - General Protect Dis-Connect Message CMD_014_0X0e
     *
     * + This message is issued by the remote device to remove protection from a destination.
     * + The controller will issue a PROTECT DIS-CONNECTED message (Command Byte 15) in response.
     *
     * | Message | Command Byte | 014 - 0x0e                                                                                                                         |
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
     * | Byte 4  | Device number| Device number MOD 128                                                                                                              |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof ProtectDisConnectMessageCommand
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
     * Builds teh extended command
     * Returns Probel SW-P-08 - Extended General Protect Dis-Connect Message CMD_142_0X8e
     *
     * + This message is issued by the remote device to remove protection from a destination.
     * + The controller will issue an EXTENDED PROTECT DIS-CONNECTED message (Command Byte 143) in response.
     *
     * | Message |  Command Byte   | 142 - 0x8e                                                                                                                      |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format    | Notes                                                                                                                           |
     * | Byte 1  | Matrix number   |                                                                                                                                 |
     * | Byte 2  | Level number    |                                                                                                                                 |
     * | Byte 3  | Dest multiplier | Destination number DIV 256                                                                                                      |
     * | Byte 4  | Dest number     | Destination number MOD 256                                                                                                      |
     * | Byte 5  | Device multiplier| Source number DIV 256                                                                                                          |
     * | Byte 6  | Device number   | Source number MOD 256                                                                                                           |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof ProtectDisConnectMessageCommand
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
