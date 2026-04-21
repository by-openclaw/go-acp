import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifier, CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandParamsUtility, CrossPointInterrogateMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the Crosspoint Interrogate Message command
 *
 * This message is a request for Tally information by matrix no., level and destination
 *
 * Command issued by the remote device
 *
 * @export
 * @class CrossPointInterrogateMessageCommand
 * @extends {CommandBase<CrossPointInterrogateMessageCommandParams>}
 */
export class CrossPointInterrogateMessageCommand extends CommandBase<CrossPointInterrogateMessageCommandParams, any> {
    /**
     * Creates an instance of CrossPointInterrogateMessageCommand
     *
     * @param {CrossPointInterrogateMessageCommandParams} params the command parameters
     * @memberof CrossPointInterrogateMessageCommand
     */
    constructor(params: CrossPointInterrogateMessageCommandParams) {
        super(CrossPointInterrogateMessageCommand.getCommandId(params), params);
        this.validateParams(params);
    }

    /**
     * Gets a boolean indicating whether the command is "General" or "Extended"
     * + General  : false => matrixId and levelId are 4 bits coded and the multiplier of (destinationId / 128) must be smaller than 7 (3 bits coded)
     * + Extended : true
     *
     * @private
     * @static
     * @param {CrossPointInterrogateMessageCommandParams} params the command parameters
     * @returns {boolean} 'true' if the command is extended otherwise 'false'
     * @memberof CrossPointInterrogateMessageCommand
     */
    private static isExtended(params: CrossPointInterrogateMessageCommandParams): boolean {
        // 895 DIV 128 < 7 (3 bits coded)
        if (params.destinationId > 895) {
            return true;
        }
        // General Command is 4 bits = 16 range [0-15]
        if (params.matrixId > 15) {
            return true;
        }
        // 4 bits = 16 range [0-15]
        if (params.levelId > 15) {
            return true;
        }
        return false;
    }

    /**
     * Gets the Command Identifier based on the state of isExtended
     *
     * @private
     * @static
     * @param {CrossPointInterrogateMessageCommandParams} params the command parameters
     * @returns {number} the command identifier
     * @memberof CrossPointInterrogateMessageCommand
     */
    private static getCommandId(params: CrossPointInterrogateMessageCommandParams): CommandIdentifier {
        return CrossPointInterrogateMessageCommand.isExtended(params)
            ? CommandIdentifiers.RX.EXTENDED.CROSSPOINT_INTERROGATE_MESSAGE
            : CommandIdentifiers.RX.GENERAL.CROSSPOINT_INTERROGATE_MESSAGE;
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof CrossPointInterrogateMessageCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)}`;
        return CrossPointInterrogateMessageCommand.isExtended(this.params)
            ? descriptionFor('Extended')
            : descriptionFor('General');
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof CrossPointInterrogateMessageCommand
     */
    protected buildData(): Buffer {
        return CrossPointInterrogateMessageCommand.isExtended(this.params)
            ? this.buildDataExtended()
            : this.buildDataNormal();
    }

    /**
     * Validates the command parameters and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {CrossPointInterrogateMessageCommandParams} params the command parameters
     * @memberof CrossPointInterrogateMessageCommand
     */
    private validateParams(params: CrossPointInterrogateMessageCommandParams): void {
        const validationErrors: Record<string, LocaleData> = new CommandParamsValidator(params).validate();

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }

    /**
     * Build the General command
     * Returns Probel SW-P-08 - General Crosspoint Interrogate Message CMD_001_0X01
     *
     * + This message is a request for Tally information by matrix no., level and destination, issued by the remote device.
     * + The controller will respond to this message with a CROSSPOINT TALLY message (normal or extended) (Command Bytes 03 or 131).
     *
     * | Message | Command Byte | 001 - 0x01                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  |              | Matrix/Level number                                                                                                                |
     * |         | Bits[4-7]    | Matrix Number                                                                                                                      |
     * |         | Bits[0-3]    | Level Number                                                                                                                       |
     * | Byte 2  | Multiplier   | this field allows sources and dests of up to 1023 to be used and provides source status info from a TDM or HD Digital Video router |
     * |         |              | 0                                                                                                                                  |
     * |         | Bit[7]       | Dest number DIV 128                                                                                                                |
     * |         | Bits[4-6]    | TDM and Digital Video source "bad" status (0 = good source)                                                                        |
     * |         | Bit[3]       | Source number DIV 128                                                                                                              |
     * |         | Bits[0-2]    |                                                                                                                                    |
     * | Byte 3  | Dest number  | Destination number MOD 128                                                                                                         |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof CrossPointInterrogateMessageCommand
     */
    private buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 4 })
            .writeUInt8(this.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(this.params.matrixId, this.params.levelId))
            .writeUInt8(BufferUtility.combine2BytesMultiplierMsbLsb(this.params.destinationId, 0))
            .writeUInt8(this.params.destinationId % 128)
            .toBuffer();
    }

    /**
     * Build the extended command
     * Returns Probel SW-P-08 - Extended General Crosspoint Interrogate Message CMD_129_0X81
     *
     * + This message is a request for Tally information by matrix no., level and destination, issued by the remote device.
     * + The controller will respond to this message with an EXTENDED CROSSPOINT TALLY message (Command Byte 131).
     *
     * | Message |  Command Byte   | 129 - 0x81                                                                                                                      |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format    | Notes                                                                                                                           |
     * | Byte 1  | Matrix number   |                                                                                                                                 |
     * | Byte 2  | Level number    |                                                                                                                                 |
     * | Byte 3  | Dest multiplier | Destination number DIV 256                                                                                                      |
     * | Byte 4  | Dest number     | Destination number MOD 256                                                                                                      |
     * @private
     * @returns {Buffer} the command message
     * @memberof CrossPointInterrogateMessageCommand
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
