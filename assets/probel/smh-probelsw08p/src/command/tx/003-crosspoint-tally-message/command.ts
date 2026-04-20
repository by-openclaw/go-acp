import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifier, CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandParamsUtility, CrossPointTallyMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the CrossPoint Tally Message command
 *
 * This message returns router tally information in response to a CROSSPOINT INTERROGATE message.
 *
 * Command issued by Pro-Bel Controller
 *
 * @export
 * @class CrossPointTallyMessageCommand
 * @extends {CommandBase<CrossPointTallyMessageCommandParams>}
 */
export class CrossPointTallyMessageCommand extends CommandBase<CrossPointTallyMessageCommandParams, any> {
    /**
     * Creates an instance of CrossPointTallyMessageCommand.
     *
     * @param {CrossPointTallyMessageCommandParams} params the command parameters
     * @memberof CrossPointTallyMessageCommand
     */
    constructor(params: CrossPointTallyMessageCommandParams) {
        super(CrossPointTallyMessageCommand.getCommandId(params), params);
        this.validateParams(params);
        this.params.statusId = 0; // For future use. We forced to "0" by default.
    }

    /**
     * Gets a boolean indicating whether the command is "General" or "Extended"
     * + General  : false => matrixId and LevelId are 4 bits coded and the multiplier of (destinationId / 128) must be smaller than 7 (3 bits coded)
     * + Extended : true
     *
     * @private
     * @static
     * @param {CrossPointTallyMessageCommandParams} params the command parameters
     * @returns {boolean} 'true' if the command is extended otherwise 'false'
     * @memberof CrossPointTallyMessageCommand
     */
    private static isExtended(params: CrossPointTallyMessageCommandParams): boolean {
        // 895 DIV 128 < 7 (3 bits coded)
        if (params.destinationId > 895) {
            return true;
        }
        //
        if (params.sourceId > 1023) {
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
     * @private
     * @static
     * @param {CrossPointTallyMessageCommandParams} params the command parameters
     * @returns {number} the command identifier
     * @memberof CrossPointTallyMessageCommand
     */
    private static getCommandId(params: CrossPointTallyMessageCommandParams): CommandIdentifier {
        return CrossPointTallyMessageCommand.isExtended(params)
            ? CommandIdentifiers.TX.EXTENDED.CROSSPOINT_TALLY_MESSAGE
            : CommandIdentifiers.TX.GENERAL.CROSSPOINT_TALLY_MESSAGE;
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof CrossPointTallyMessageCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string, withStatusId: boolean): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params, withStatusId)}`;

        return CrossPointTallyMessageCommand.isExtended(this.params)
            ? descriptionFor('Extended', true)
            : descriptionFor('General', false);
    }

    /**
     * Build the Pro-Bel SW-P-8 command CrossPoint Tally Message
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof CrossPointTallyMessageCommand
     */
    protected buildData(): Buffer {
        return CrossPointTallyMessageCommand.isExtended(this.params)
            ? this.buildDataExtended()
            : this.buildDataNormal();
    }

    /**
     * Validates the command parameters and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {CrossPointTallyMessageCommandParams} params the command parameters
     * @memberof CrossPointTallyMessageCommand
     */
    private validateParams(params: CrossPointTallyMessageCommandParams): void {
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
     * Returns Probel SW-P-08 - General CrossPoint Tally Message CMD_003_0X03
     *
     * + This message returns router tally information in response to a CROSSPOINT INTERROGATE message (Command Byte 01).
     *
     *   | Message | Command Byte | 003 - 0x03                                                                                                                         |
     *   |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     *   | Byte    | Field Format | Notes                                                                                                                              |
     *   | Byte 1  |              | Matrix/Level number                                                                                                                |
     *   |         | Bits[4-7]    | Matrix Number                                                                                                                      |
     *   |         | Bits[0-3]    | Level Number                                                                                                                       |
     *   | Byte 2  | Multiplier   | this field allows sources and dests of up to 1023 to be used and provides source status info from a TDM or HD Digital Video router |
     *   |         |              | 0                                                                                                                                  |
     *   |         | Bit[7]       | Dest number DIV 128                                                                                                                |
     *   |         | Bits[4-6]    | TDM and Digital Video source "bad" status (0 = good source)                                                                        |
     *   |         | Bit[3]       | Source number DIV 128                                                                                                              |
     *   |         | Bits[0-2]    |                                                                                                                                    |
     *   | Byte 3  | Dest number  | Destination number MOD 128                                                                                                         |
     *   | Byte 4  | Src  number  | Source number MOD 128                                                                                                              |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof CrossPointTallyMessageCommand
     */
    private buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 5 })
            .writeUInt8(this.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(this.params.matrixId, this.params.levelId))
            .writeUInt8(BufferUtility.combine2BytesMultiplierMsbLsb(this.params.destinationId, this.params.sourceId))
            .writeUInt8(this.params.destinationId % 128)
            .writeUInt8(this.params.sourceId % 128)
            .toBuffer();
    }

    /**
     * Builds the extended command
     * Returns Probel SW-P-08 - Extended General CrossPoint Tally Message CMD_131_0X83
     *
     * + This message returns router tally information in response to an EXTENDED CROSSPOINT INTERROGATE message (Command Byte 129).
     *
     *   | Message |  Command Byte   | 131 - 0x83                                                                                                                      |
     *   |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     *   | Byte    | Field Format    | Notes                                                                                                                           |
     *   | Byte 1  | Matrix number   |                                                                                                                                 |
     *   | Byte 2  | Level number    |                                                                                                                                 |
     *   | Byte 3  | Dest multiplier | Destination number DIV 256                                                                                                      |
     *   | Byte 4  | Dest number     | Destination number MOD 256                                                                                                      |
     *   | Byte 5  | Src multiplier  | Source number DIV 256                                                                                                           |
     *   | Byte 6  | Src number      | Source number MOD 256                                                                                                           |
     *   | Byte 7  | Status          | (For future use)                                                                                                                |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof CrossPointTallyMessageCommand
     */
    private buildDataExtended(): Buffer {
        const buffer = new SmartBuffer({ size: 8 })
            .writeUInt8(this.id)
            .writeUInt8(this.params.matrixId)
            .writeUInt8(this.params.levelId)
            .writeUInt8(Math.floor(this.params.destinationId / 256))
            .writeUInt8(this.params.destinationId % 256)
            .writeUInt8(Math.floor(this.params.sourceId / 256))
            .writeUInt8(this.params.sourceId % 256)
            .writeUInt8(this.params.statusId);
        return buffer.toBuffer();
    }
}
