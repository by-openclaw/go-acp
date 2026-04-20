import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifier, CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandParamsUtility, CrossPointTallyDumpRequestMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the CrossPoint Tally Dump Request Message command
 *
 * This message is issued by the remote device to request a tally table dump for a given matrix/level combination.
 * The controller will respond with CROSSPOINT TALLY DUMP (byte & word) messages (Command Bytes 22 & 23 dependent on matrix size).
 *
 * Command issued by the remote device
 * @export
 * @class CrossPointTallyDumpRequestMessageCommand
 * @extends {CommandBase<CrossPointTallyDumpRequestMessageCommandParams>}
 */
export class CrossPointTallyDumpRequestMessageCommand extends CommandBase<
    CrossPointTallyDumpRequestMessageCommandParams,
    any
> {
    /**
     * Creates an instance of CrossPointTallyDumpRequestMessageCommand
     *
     * @param {CrossPointTallyDumpRequestMessageCommandParams} params the command parameters
     * @memberof CrossPointTallyDumpRequestMessageCommand
     */
    constructor(params: CrossPointTallyDumpRequestMessageCommandParams) {
        super(CrossPointTallyDumpRequestMessageCommand.getCommandId(params), params);
        this.validateParams(params);
    }

    /**
     * Gets a boolean indicating whether the command is "General" or "Extended"
     * + General  : false => matrixId and LevelId are 4 bits coded
     * + Extended : true
     *
     * @private
     * @static
     * @param {CrossPointTallyDumpRequestMessageCommandParams} params the command parameters
     * @returns {boolean} 'true' is the command is extended otherwise 'false'
     * @memberof CrossPointTallyDumpRequestMessageCommand
     */
    private static isExtended(params: CrossPointTallyDumpRequestMessageCommandParams): boolean {
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
     * @param {CrossPointTallyDumpRequestMessageCommandParams} params the command parameters
     * @returns {number} the command identifier
     * @memberof CrossPointTallyDumpRequestMessageCommand
     */
    private static getCommandId(params: CrossPointTallyDumpRequestMessageCommandParams): CommandIdentifier {
        return CrossPointTallyDumpRequestMessageCommand.isExtended(params)
            ? CommandIdentifiers.RX.EXTENDED.CROSSPOINT_TALLY_DUMP_REQUEST_MESSAGE
            : CommandIdentifiers.RX.GENERAL.CROSSPOINT_TALLY_DUMP_REQUEST_MESSAGE;
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof CrossPointTallyDumpRequestMessageCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)}`;

        return CrossPointTallyDumpRequestMessageCommand.isExtended(this.params)
            ? descriptionFor('Extended')
            : descriptionFor('General');
    }

    /**
     * Build the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof CrossPointTallyDumpRequestMessageCommand
     */
    protected buildData(): Buffer {
        return CrossPointTallyDumpRequestMessageCommand.isExtended(this.params)
            ? this.buildDataExtended()
            : this.buildDataNormal();
    }

    /**
     * Validates the command parameters and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {CrossPointTallyDumpRequestMessageCommandParams} params the command parameters
     * @memberof CrossPointTallyDumpRequestMessageCommand
     */
    private validateParams(params: CrossPointTallyDumpRequestMessageCommandParams): void {
        const validationErrors: Record<string, LocaleData> = new CommandParamsValidator(params).validate();

        if (Object.keys(validationErrors).length > 0) {
            const message = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(CommandErrorsKeys.CREATE_COMMAND_FAILURE)
                .description;
            throw new ValidationError(message, validationErrors);
        }
    }

    /**
     * Builds the normal command
     * Returns Probel SW-P-08 - General CrossPoint Tally Dump Request Message CMD_021_0X15
     *
     * + This message is issued by the remote device to request a tally table dump for a given matrix/level combination.
     * + The controller will respond with CROSSPOINT TALLY DUMP (byte & word) messages (Command Bytes 22 & 23 dependent on matrix size).
     *
     * | Message | Command Byte | 021 - 0x15                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  |              | Matrix/Level number                                                                                                                |
     * |         | Bits[4-7]    | Matrix Number                                                                                                                      |
     * |         | Bits[0-3]    | Level Number                                                                                                                       |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof CrossPointTallyDumpRequestMessageCommand
     */
    private buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 2 })
            .writeUInt8(this.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(this.params.matrixId, this.params.levelId))
            .toBuffer();
    }

    /**
     * Builds the extended command
     * Returns Probel SW-P-08 - Extended General CrossPoint Tally Dump Request Message CMD_149_0X95
     *
     * + This message is issued by the remote device to request a tally table dump for a given matrix/level combination.
     * + The controller will respond with an EXTENDED CROSSPOINT TALLY DUMP (word) message (Command Byte 151).
     *
     * | Message |  Command Byte   | 149 - 0x95                                                                                                                      |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format    | Notes                                                                                                                           |
     * | Byte 1  | Matrix number   |                                                                                                                                 |
     * | Byte 2  | Level number    |                                                                                                                                 |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof CrossPointTallyDumpRequestMessageCommand
     */
    private buildDataExtended(): Buffer {
        return new SmartBuffer({ size: 3 })
            .writeUInt8(this.id)
            .writeUInt8(this.params.matrixId)
            .writeUInt8(this.params.levelId)
            .toBuffer();
    }
}
