import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifier, CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { CommandOptionsUtility, SingleSourceNamesRequestMessageCommandOptions } from './options';
import { CommandParamsUtility, SingleSourceNamesRequestMessageCommandParams } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the Single Source Association Names Request Message command
 *
 * This message is issued by the remote device to request the name for a single source association for a given matrix.
 * The controller will respond with one SOURCE ASSOCIATION NAMES RESPONSE message (Command Byte 116).
 *
 * Command issued by the remote device
 *
 * @export
 * @class SingleSourceNamesRequestMessageCommand
 * @extends {CommandBase<SingleSourceNamesRequestMessageCommandParams>}
 */
export class SingleSourceNamesRequestMessageCommand extends CommandBase<
    SingleSourceNamesRequestMessageCommandParams,
    SingleSourceNamesRequestMessageCommandOptions
> {
    /**
     * Creates an instance of SingleSourceNamesRequestMessageCommand
     *
     * @param {SingleSourceNamesRequestMessageCommandParams} params the command parameters
     * @param {SingleSourceNamesRequestMessageCommandOptions} _options the command options
     * @memberof SingleSourceNamesRequestMessageCommand
     */
    constructor(
        params: SingleSourceNamesRequestMessageCommandParams,
        options: SingleSourceNamesRequestMessageCommandOptions
    ) {
        super(SingleSourceNamesRequestMessageCommand.getCommandId(params), params, options);
        this.validateParams(params);
    }

    /**
     * Gets a boolean indicating whether the command is "General" or "Extended"
     * + General  : false => matrixId and LevelId are 4 bits coded
     * + Extended : true
     *
     * @private
     * @static
     * @param {SingleSourceNamesRequestMessageCommandParams} params the command parameters
     * @returns {boolean} 'true' is the command is extended otherwise 'false'
     * @memberof SingleSourceNamesRequestMessageCommand
     */
    private static isExtended(params: SingleSourceNamesRequestMessageCommandParams): boolean {
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
     * @param {AllSourceNamesRequestMessageCommandParams} params the command parameters
     * @returns {number} the command identifier
     * @memberof AllSourceNamesRequestMessageCommand
     */
    private static getCommandId(params: SingleSourceNamesRequestMessageCommandParams): CommandIdentifier {
        return SingleSourceNamesRequestMessageCommand.isExtended(params)
            ? CommandIdentifiers.RX.EXTENDED.SINGLE_SOURCE_NAMES_REQUEST_MESSAGE
            : CommandIdentifiers.RX.GENERAL.SINGLE_SOURCE_NAME_REQUEST_MESSAGE;
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {string} the command textual representation
     * @memberof SingleSourceNamesRequestMessageCommand
     */
    toLogDescription(): string {
        const descriptionFor = (type: string): string =>
            `${type} - ${this.name}: ${CommandParamsUtility.toString(this.params)}, ${CommandOptionsUtility.toString(
                this.options
            )}`;

        return SingleSourceNamesRequestMessageCommand.isExtended(this.params)
            ? descriptionFor('Extended')
            : descriptionFor('General');
    }

    /**
     * Build the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof SingleSourceNamesRequestMessageCommand
     */
    protected buildData(): Buffer {
        return SingleSourceNamesRequestMessageCommand.isExtended(this.params)
            ? this.buildDataExtended()
            : this.buildDataNormal();
    }

    /**
     * Validates the command parameters, options and throws a ValidationError in error(s) occur
     *
     * @private
     * @param {SingleSourceNamesRequestMessageCommandParams} params the command parameters
     * @param {SingleSourceNamesRequestMessageCommandOptions} options the command options
     * @memberof SingleSourceNamesRequestMessageCommand
     */
    private validateParams(params: SingleSourceNamesRequestMessageCommandParams): void {
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
     * Returns Probel SW-P-08 - General Single Source Name Request Message CMD_101_0X65
     *
     * + This message is issued by the remote device to request the name for a single source.
     * + The controller will respond with a single SOURCE NAME RESPONSE message (Command Byte 106).
     *
     * | Message | Command Byte | 101 - 0x65                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  |              | Matrix/Level number                                                                                                                |
     * |         | Bits[4-7]    | Matrix Number                                                                                                                      |
     * |         | Bits[0-3]    | Level Number                                                                                                                       |
     * | Byte 2  |Name Length   | Length of Names Required                                                                                                           |
     * |         | 0            | 4 char names                                                                                                                       |
     * |         | 1            | 8 char names                                                                                                                       |
     * |         | 2            | 12 char names                                                                                                                      |
     * | Byte 3  | Src multiplier| Source number DIV 256                                                                                                             |
     * | Byte 4  | Src  number  | Source number MOD 256                                                                                                              |
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof SingleSourceNamesRequestMessageCommand
     */
    private buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 5 })
            .writeUInt8(this.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(this.params.matrixId, this.params.levelId))
            .writeUInt8(this.options.lengthOfNames)
            .writeUInt8(Math.floor(this.params.sourceId / 256))
            .writeUInt8(this.params.sourceId % 256)
            .toBuffer();
    }

    /**
     * Builds thr extended command
     * Returns Probel SW-P-08 - Extended General Single Source Name Request Message CMD_229_0Xe5
     *
     * + This message is issued by the remote device to request the name for a single source.
     * + The controller will respond with a single EXTENDED SOURCE NAME RESPONSE message (Command Byte 234).
     *
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Message | Command Byte | 229 - 0xe5                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte 1  | Matrix number|                                                                                                                                    |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte 2  | Level number |                                                                                                                                    |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte 3  |Name Length   | Length of Names Required                                                                                                           |
     * |         | 0            | 4 char names                                                                                                                       |
     * |         | 1            | 8 char names                                                                                                                       |
     * |         | 2            | 12 char names                                                                                                                      |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte 4  | Src multiplier| Source number DIV 256                                                                                                             |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte 5  | Src  number  | Source number MOD 256                                                                                                              |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     *
     * @private
     * @returns {Buffer} the command message
     * @memberof SingleSourceNamesRequestMessageCommand
     */
    private buildDataExtended(): Buffer {
        return new SmartBuffer({ size: 6 })
            .writeUInt8(this.id)
            .writeUInt8(this.params.matrixId)
            .writeUInt8(this.params.levelId)
            .writeUInt8(this.options.lengthOfNames)
            .writeUInt8(Math.floor(this.params.sourceId / 256))
            .writeUInt8(this.params.sourceId % 256)
            .toBuffer();
    }
}
