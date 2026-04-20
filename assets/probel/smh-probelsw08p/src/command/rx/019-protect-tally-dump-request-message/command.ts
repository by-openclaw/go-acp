import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifier, CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandParamsUtility, ProtectTallyDumpRequestMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the Protect Tally Dump Request Message command
 *
 * + This message allows all the Protect information to be requested from the Pro-Bel Controller when a Remote device initialises.
 * + The controller will respond with a PROTECT TALLY DUMP message (Command Byte 20).
 *
 * Command issued by the remote device
 * @export
 * @class ProtectTallyDumpRequestMessageCommand
 * @extends {CommandBase<ProtectTallyDumpRequestMessageCommandParams>}
 */
export class ProtectTallyDumpRequestMessageCommand extends CommandBase<
    ProtectTallyDumpRequestMessageCommandParams,
    any
> {
    /**
     * Creates an instance of ProtectTallyDumpRequestMessageCommand
     *
     * @param {ProtectTallyDumpRequestMessageCommandParams} params the command parameters
     * @memberof ProtectTallyDumpRequestMessageCommand
     */
    constructor(params: ProtectTallyDumpRequestMessageCommandParams) {
        super(ProtectTallyDumpRequestMessageCommand.getCommandId(params), params);
        this.validateParams(params);
    }

    /**
     * Gets a boolean indicating whether the command is "General" or "Extended"
     * + General  : false => matrixId and LevelId are 4 bits coded and the multiplier of (destinationId / 128) must be smaller than 7 (3 bits coded)
     * + Extended : true
     *
     * @private
     * @static
     * @param {ProtectTallyDumpRequestMessageCommandParams} params the command parameters
     * @returns {boolean} 'true' is the command is extended otherwise 'false'
     * @memberof ProtectTallyDumpRequestMessageCommand
     */
    private static isExtended(params: ProtectTallyDumpRequestMessageCommandParams): boolean {
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
     * @param {ProtectTallyDumpRequestMessageCommandParams} params the command parameters
     * @returns {number} the command identifier
     * @memberof ProtectTallyDumpRequestMessageCommand
     */
    private static getCommandId(params: ProtectTallyDumpRequestMessageCommandParams): CommandIdentifier {
        return ProtectTallyDumpRequestMessageCommand.isExtended(params)
            ? CommandIdentifiers.RX.EXTENDED.PROTECT_TALLY_DUMP_REQUEST_MESSAGE
            : CommandIdentifiers.RX.GENERAL.PROTECT_TALLY_DUMP_REQUEST_MESSAGE;
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string}  the command textual representation
     * @memberof ProtectTallyDumpRequestMessageCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)}`;

        return ProtectTallyDumpRequestMessageCommand.isExtended(this.params)
            ? descriptionFor('Extended')
            : descriptionFor('General');
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof ProtectTallyDumpRequestMessageCommand
     */
    protected buildData(): Buffer {
        return ProtectTallyDumpRequestMessageCommand.isExtended(this.params)
            ? this.buildDataExtended()
            : this.buildDataNormal();
    }

    /**
     * Validates the command parameters and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {ProtectTallyDumpRequestMessageCommandParams} params the command parameters
     * @memberof ProtectTallyDumpRequestMessageCommand
     */
    private validateParams(params: ProtectTallyDumpRequestMessageCommandParams): void {
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
     * Returns Probel SW-P-08 - General Protect Tally Dump Request Message CMD_019_0X13
     *
     * + This message allows all the Protect information to be requested from the Pro-Bel Controller when a Remote device initialises.
     * + The controller will respond with a PROTECT TALLY DUMP message (Command Byte 20).
     *
     * | Message | Command Byte | 019 - 0x13                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  |              | Matrix/Level number                                                                                                                |
     * |         | Bits[4-7]    | Matrix Number                                                                                                                      |
     * |         | Bits[0-3]    | Level Number                                                                                                                       |
     * | Byte 2  | Dest numnber | Destination number DIV 256                                                                                                         |
     * | Byte 3  | Dest number  | Destination number MOD 256                                                                                                         |
     *
     * @private
     * @returns {Buffer} the message command
     * @memberof ProtectTallyDumpRequestMessageCommand
     */
    private buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 4 })
            .writeUInt8(this.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(this.params.matrixId, this.params.levelId))
            .writeUInt8(Math.floor(this.params.destinationId / 256))
            .writeUInt8(this.params.destinationId % 256)
            .toBuffer();
    }

    /**
     * Builds the extended command
     * Returns Probel SW-P-08 - Extended General Protect Tally Dump Request Message CMD_147_0X93
     *
     * + This message allows all the Protect information to be requested from the Pro-Bel Controller when a Remote device initialises.
     * + The controller will respond with an EXTENDED PROTECT TALLY DUMP message (Command Byte 148).
     *
     * | Message |  Command Byte   | 147 - 0x93                                                                                                                      |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format    | Notes                                                                                                                           |
     * | Byte 1  | Matrix number   |                                                                                                                                 |
     * | Byte 2  | Level number    |                                                                                                                                 |
     * | Byte 3  | Dest multiplier | Destination number DIV 256                                                                                                      |
     * | Byte 4  | Dest number     | Destination number MOD 256                                                                                                      |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof ProtectTallyDumpRequestMessageCommand
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
