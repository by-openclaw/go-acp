import { SmartBuffer } from 'smart-buffer';

import { LocaleData } from '../../../common/locale-data/locale-data.model';
import { BufferUtility } from '../../../common/utility/buffer.utility';
import { CommandIdentifiers, ValidationError } from '../../command-contract';
import { LocaleDataCache } from '../../command-locale-data-cache';
import { CommandBase } from '../../command.base';
import { CommandErrorsKeys } from '../../locale-data-keys';
import { AllSourceAssociationNamesRequestMessageCommandOptions, CommandOptionsUtility } from './options';
import { AllSourceAssociationNamesRequestMessageCommandParams, CommandParamsUtility } from './params';
import { CommandParamsValidator } from './params.validator';

/**
 * Implements the All Source Names Request Message command
 *
 * This message is issued by the remote device to request the names for all the source associations for a given matrix.
 * The controller will respond with one or more SOURCE ASSOCIATION NAMES RESPONSE messages (Command Byte 116).
 *
 * + Command issued by the remote device
 * @export
 * @class AllSourceAssociationNamesRequestMessageCommand
 * @extends {CommandBase<AllSourceAssociationNamesRequestMessageCommandParams>}
 */
export class AllSourceAssociationNamesRequestMessageCommand extends CommandBase<
    AllSourceAssociationNamesRequestMessageCommandParams,
    AllSourceAssociationNamesRequestMessageCommandOptions
> {
    /**
     * Creates an instance of AllSourceAssociationNamesRequestMessageCommand
     *
     * @param {AllSourceAssociationNamesRequestMessageCommandParams} params the command parameters
     * @param {AllSourceAssociationNamesRequestMessageCommandOptions} _options the command options
     * @memberof AllSourceAssociationNamesRequestMessageCommand
     */
    constructor(
        params: AllSourceAssociationNamesRequestMessageCommandParams,
        options: AllSourceAssociationNamesRequestMessageCommandOptions
    ) {
        super(CommandIdentifiers.RX.GENERAL.ALL_SOURCE_ASSOCIATION_NAMES_REQUEST_MESSAGE, params, options);
        this.validateParams(params);
    }

    /**
     * Gets a textual representation of the command including params (mainly used for logging purpose)
     *
     * @returns {number} the command textual representation
     * @memberof AllSourceAssociationNamesRequestMessageCommand
     */
    toLogDescription(): string {
        return `General - ${this.name}: ${CommandParamsUtility.toString(this.params)}, ${CommandOptionsUtility.toString(
            this.options
        )}`;
    }

    /**
     * Builds the command
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof AllSourceAssociationNamesRequestMessageCommand
     */
    protected buildData(): Buffer {
        return this.buildDataNormal();
    }

    /**
     * Build the general command
     * Returns Probel SW-P-08 - General All Source Names Request Message CMD_114_0X72
     *
     * + This message is issued by the remote device to request the names for all the source associations for a given matrix.
     * + The controller will respond with one or more SOURCE ASSOCIATION NAMES RESPONSE messages (Command Byte 116).
     *
     * | Message | Command Byte | 114 - 0x72                                                                                                                         |
     * |---------|--------------|------------------------------------------------------------------------------------------------------------------------------------|
     * | Byte    | Field Format | Notes                                                                                                                              |
     * | Byte 1  |              | Matrix/Level number                                                                                                                |
     * |         | Bits[4-7]    | Matrix Number                                                                                                                      |
     * |         | Bits[0-3]    | 0                                                                                                                                  |
     * | Byte 2  |Name Length   | Length of Names Required                                                                                                           |
     * |         | 0            | 4 char names                                                                                                                       |
     * |         | 1            | 8 char names                                                                                                                       |
     * |         | 2            | 12 char names                                                                                                                      |
     *
     * @protected
     * @returns {Buffer} the command message
     * @memberof AllSourceAssociationNamesRequestMessageCommand
     */
    protected buildDataNormal(): Buffer {
        return new SmartBuffer({ size: 3 })
            .writeUInt8(this.id)
            .writeUInt8(BufferUtility.combine2BytesMsbLsb(this.params.matrixId, 0))
            .writeUInt8(this.options.lengthOfNames)
            .toBuffer();
    }

    /**
     * Validate the parameters, options and throw a ValidationError in case of error
     *
     * @private
     * @param {AllSourceAssociationNamesRequestMessageCommandParams} params the command parameters
     * @param {AllSourceAssociationNamesRequestMessageCommandOptions} options the command options
     * @memberof AllSourceAssociationNamesRequestMessageCommand
     */
    private validateParams(params: AllSourceAssociationNamesRequestMessageCommandParams): void {
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
}
