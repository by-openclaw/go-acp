import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifier, CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { AllSourceNamesRequestMessageCommandOptions, CommandOptionsUtility } from './options';
import { AllSourceNamesRequestMessageCommandParams, CommandParamsUtility } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the All Source Names Request Message command
 *
 * This message is issued by the remote device to request the names for all the sources on a given matrix and level.
 * The controller will respond with one or more SOURCE NAME RESPONSE messages (Command Byte 106).
 *
 * Command issued by the remote device
 * @export
 * @class AllSourceNamesRequestMessageCommand
 * @extends {CommandBase<AllSourceNamesRequestMessageCommandParams>}
 */
export class AllSourceNamesRequestMessageCommand extends CommandBase<
    AllSourceNamesRequestMessageCommandParams,
    AllSourceNamesRequestMessageCommandOptions
> {
    /**
     * Creates an instance of AllSourceNamesRequestMessageCommand
     *
     * @param {AllSourceNamesRequestMessageCommandParams} params the command parameters
     * @param {AllSourceNamesRequestMessageCommandOptions} _options the command options
     * @memberof AllSourceNamesRequestMessageCommand
     */
    constructor(
        params: AllSourceNamesRequestMessageCommandParams,
        options: AllSourceNamesRequestMessageCommandOptions
    ) {
        super(AllSourceNamesRequestMessageCommand.getCommandId(params), params, options);
        this.validateParams(params);
    }

    /**
     * Gets a boolean indicating whether the command is "General" or "Extended"
     * + General  : false => matrixId and LevelId are 4 bits coded
     * + Extended : true
     *
     * @private
     * @static
     * @param {AllSourceNamesRequestMessageCommandParams} params the command parameters
     * @returns {boolean} 'true' is the command is extended otherwise 'false'
     * @memberof AllSourceNamesRequestMessageCommand
     */
    private static isExtended(params: AllSourceNamesRequestMessageCommandParams): boolean {
        return params.matrixId > 15 || params.levelId > 15;
    }

    /**
     * Gets the Command Identifier based on the state of isExtended
     *
     * @private
     * @static
     * @param {AllSourceNamesRequestMessageCommandParams} params the command parameters
     * @returns {number} the command identifier
     * @memberof AllSourceNamesRequestMessageCommand
     */
    private static getCommandId(params: AllSourceNamesRequestMessageCommandParams): CommandIdentifier {
        return AllSourceNamesRequestMessageCommand.isExtended(params)
            ? CommandIdentifiers.RX.EXTENDED.ALL_SOURCE_NAMES_REQUEST_MESSAGE
            : CommandIdentifiers.RX.GENERAL.ALL_SOURCE_NAMES_REQUEST_MESSAGE;
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof AllSourceNamesRequestMessageCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)}, ${CommandOptionsUtility.toString(
                this.options
            )}`;

        return AllSourceNamesRequestMessageCommand.isExtended(this.params)
            ? descriptionFor('Extended')
            : descriptionFor('General');
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof AllSourceNamesRequestMessageCommand
     */
    protected buildData(): Buffer {
        return AllSourceNamesRequestMessageCommand.isExtended(this.params)
            ? this.buildDataExtended()
            : this.buildDataNormal();
    }

    /**
     * Validates the command parameters, options and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {AllSourceNamesRequestMessageCommandParams} params the command parameters
     * @memberof AllSourceNamesRequestMessageCommand
     */
    private validateParams(params: AllSourceNamesRequestMessageCommandParams): void {
        const validationErrors: Record<string, LocaleData> = {
            ...new CommandParamsValidator(params).validate()
        };

        if (Object.keys(validationErrors).length > 0) {
            const localeData = LocaleDataCache.INSTANCE.getCommandErrorLocaleData(
                CommandErrorsKeys.CREATE_COMMAND_FAILURE
            );
            throw new ValidationError(localeData.description, validationErrors);
        }
    }

    /**
     * Builds the normal command
     * Returns Probel SW-P-08 - General All Source Names Request Message CMD_100_0X64
     *
     * + This message is issued by the remote device to request the names for all the sources on a given matrix and level.
     * + The controller will respond with one or more SOURCE NAME RESPONSE messages (Command Byte 106).
     *
     * | Message | Command Byte | 100 - 0x64                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  |              | Matrix/Level number                                                                                                                |
     * |         | Bits[4-7]    | Matrix Number                                                                                                                      |
     * |         | Bits[0-3]    | Level Number                                                                                                                       |
     * | Byte 2  |Name Length   | Length of Names Required                                                                                                           |
     * |         | 0            | 4 char names                                                                                                                       |
     * |         | 1            | 8 char names                                                                                                                       |
     * |         | 2            | 12 char names                                                                                                                      |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof AllSourceNamesRequestMessageCommand
     */
    private buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 3 })
            .writeUInt8(this.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(this.params.matrixId, this.params.levelId))
            .writeUInt8(this.options.lengthOfNames)
            .toBuffer();
    }

    /**
     * Build the extended command
     * Returns Probel SW-P-08 - Extended General General All Source Names Request Message CMD_228_0Xe4
     *
     * + This message is issued by the remote device to request the names for all the sources on a given matrix and level.
     * + The controller will respond with one or more EXTENDED SOURCE NAME RESPONSE messages (Command Byte 234).
     *
     * | Message |  Command Byte   | 228 - 0xe4                                                                                                                      |
     * |---------|-----------------|---------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format    | Notes                                                                                                                           |
     * | Byte 1  | Matrix number   |                                                                                                                                 |
     * | Byte 2  | Level number    |                                                                                                                                 |
     * | Byte 3  | Dest multiplier | Destination number DIV 256                                                                                                      |
     * | Byte 4  | Dest number     | Destination number MOD 256                                                                                                      |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof AllSourceNamesRequestMessageCommand
     */
    private buildDataExtended(): Buffer {
        return new SmartBuffer({ size: 4 })
            .writeUInt8(this.id)
            .writeUInt8(this.params.matrixId)
            .writeUInt8(this.params.levelId)
            .writeUInt8(this.options.lengthOfNames)
            .toBuffer();
    }
}
