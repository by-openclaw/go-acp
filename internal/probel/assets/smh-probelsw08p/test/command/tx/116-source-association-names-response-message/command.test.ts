import * as _ from 'lodash';
import { SourceAssociationNamesResponseCommand } from '../../../../src/command/tx/116-source-association-names-response-message/command';
import { SourceAssociationNamesResponseCommandParams } from '../../../../src/command/tx/116-source-association-names-response-message/params';
import { Fixture } from '../../../fixture/fixture';
import { BootstrapService } from '../../../../src/bootstrap.service';
import { ValidationError, CommandIdentifiers, CommandIdentifier } from '../../../../src/command/command-contract';
import { CommandErrorsKeys } from '../../../../src/command/locale-data-keys';
import {
    SourceAssociationNamesResponseCommandOptions
} from '../../../../src/command/tx/116-source-association-names-response-message/options';
import { NameLength } from '../../../../src/command/shared/name-length';

describe('Source Association Names Response Message', () => {
    beforeAll(async () => {
        await BootstrapService.bootstrapAsync('en');
    });

    describe('ctor', () => {
        it('Should instantiate the command with valid params, options', () => {
            // Arrange
            const params: SourceAssociationNamesResponseCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstSourceId: 0,
                numberOfSourceAssociationNamesTofollow: 32,
                sourceAssociationNameItems: Fixture.buildCharItems(64, 4)
            };
            const options: SourceAssociationNamesResponseCommandOptions = {
                lengthOfNames: NameLength.FOUR_CHAR_NAMES
            };
            // Act
            const command = new SourceAssociationNamesResponseCommand(params, options);

            // Assert
            expect(command).toBeDefined();
            expect(command.params).toBe(params);
            expect(command.options).toBe(options);
            expect(command.identifier).toBe(CommandIdentifiers.TX.GENERAL.SOURCE_ASSOCIATION_NAMES_RESPONSE_MESSAGE);
        });

        it('Should throw an error if params params are out of range < MIN', done => {
            // Arrange
            const params: SourceAssociationNamesResponseCommandParams = {
                matrixId: -1,
                levelId: -1,
                firstSourceId: -1,
                numberOfSourceAssociationNamesTofollow: 0,
                sourceAssociationNameItems: []
            };
            const options: SourceAssociationNamesResponseCommandOptions = {
                lengthOfNames: NameLength.SIXTEEN_CHAR_NAMES
            };
            // Act
            const fct = () => new SourceAssociationNamesResponseCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.firstSourceId.id).toBe(
                    CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.SourceIdAndMaximumNumberOfNames.id).toBe(
                    CommandErrorsKeys.SOURCE_ASSOCIATION_NAME_ID_AND_MAXIMUM_NUMBER_OF_SOURCE_ASSOCIATION_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceAssociationNameItems.id).toBe(
                    CommandErrorsKeys.SOURCE_NAMES_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });

        it('Should throw an error if params params are out of range > MAX', done => {
            // Arrange
            const params: SourceAssociationNamesResponseCommandParams = {
                matrixId: 256,
                levelId: 256,
                firstSourceId: 65536,
                numberOfSourceAssociationNamesTofollow: 32,
                sourceAssociationNameItems: []
            };
            const options: SourceAssociationNamesResponseCommandOptions = {
                lengthOfNames: NameLength.SIXTEEN_CHAR_NAMES
            };
            // Act
            const fct = () => new SourceAssociationNamesResponseCommand(params, options);

            // Assert
            try {
                fct();
            } catch (e) {
                expect(e).toBeInstanceOf(ValidationError);
                const localeDataError = e as ValidationError;
                expect(localeDataError.validationErrors).toBeDefined();
                expect(localeDataError.message).toBeDefined();
                expect(localeDataError.validationErrors?.matrixId.id).toBe(
                    CommandErrorsKeys.MATRIXID_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.levelId.id).toBe(
                    CommandErrorsKeys.LEVEL_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.firstSourceId.id).toBe(
                    CommandErrorsKeys.SOURCE_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.SourceIdAndMaximumNumberOfNames.id).toBe(
                    CommandErrorsKeys.SOURCE_ASSOCIATION_NAME_ID_AND_MAXIMUM_NUMBER_OF_SOURCE_ASSOCIATION_NAMES_IS_OUT_OF_RANGE_ERROR_MSG
                );
                expect(localeDataError.validationErrors?.sourceAssociationNameItems.id).toBe(
                    CommandErrorsKeys.SOURCE_NAMES_ITEMS_IS_OUT_OF_RANGE_ERROR_MSG
                );
                done();
            }
        });
    });

    describe('buildCommand', () => {
        describe('General Source Association Names Response Message CMD_116_0X74', () => {
            // let commandIdentifier: CommandIdentifier;

            // beforeAll(() => {
            //     commandIdentifier = CommandIdentifiers.TX.GENERAL.SOURCE_NAMES_RESPONSE_MESSAGE;
            // });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SourceAssociationNamesResponseCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    firstSourceId: 0,
                    numberOfSourceAssociationNamesTofollow: 32,
                    sourceAssociationNameItems: Fixture.buildCharItems(64, 4)
                };
                const options: SourceAssociationNamesResponseCommandOptions = {
                    lengthOfNames: NameLength.FOUR_CHAR_NAMES
                };
                let commandIdentifier: CommandIdentifier;
                commandIdentifier = CommandIdentifiers.TX.GENERAL.SOURCE_ASSOCIATION_NAMES_RESPONSE_MESSAGE;
                // Act
                const metaCommand = new SourceAssociationNamesResponseCommand(params, options);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            '6a 00 00 00 00 20 30 30 30 30 30 30 30 31 30 30 30 32 30 30 30 33 30 30 30 34 30 30 30 35 30 30 30 36 30 30 30 37 30 30 30 38 30 30 30 39 30 30 31 30 30 30 31 31 30 30 31 32 30 30 31 33 30 30 31 34 30 30 31 35 30 30 31 36 30 30 31 37 30 30 31 38 30 30 31 39 30 30 32 30 30 30 32 31 30 30 32 32 30 30 32 33 30 30 32 34 30 30 32 35 30 30 32 36 30 30 32 37 30 30 32 38 30 30 32 39 30 30 33 30 30 30 33 31',
                        bytesCount: 0x86,
                        checksum: 0x44,
                        buffer:
                            '10 02 6a 00 00 00 00 20 30 30 30 30 30 30 30 31 30 30 30 32 30 30 30 33 30 30 30 34 30 30 30 35 30 30 30 36 30 30 30 37 30 30 30 38 30 30 30 39 30 30 31 30 30 30 31 31 30 30 31 32 30 30 31 33 30 30 31 34 30 30 31 35 30 30 31 36 30 30 31 37 30 30 31 38 30 30 31 39 30 30 32 30 30 30 32 31 30 30 32 32 30 30 32 33 30 30 32 34 30 30 32 35 30 30 32 36 30 30 32 37 30 30 32 38 30 30 32 39 30 30 33 30 30 30 33 31 86 44 10 03'
                    },
                    {
                        data:
                            '6a 00 00 00 20 20 30 30 33 32 30 30 33 33 30 30 33 34 30 30 33 35 30 30 33 36 30 30 33 37 30 30 33 38 30 30 33 39 30 30 34 30 30 30 34 31 30 30 34 32 30 30 34 33 30 30 34 34 30 30 34 35 30 30 34 36 30 30 34 37 30 30 34 38 30 30 34 39 30 30 35 30 30 30 35 31 30 30 35 32 30 30 35 33 30 30 35 34 30 30 35 35 30 30 35 36 30 30 35 37 30 30 35 38 30 30 35 39 30 30 36 30 30 30 36 31 30 30 36 32 30 30 36 33',
                        bytesCount: 0x86,
                        checksum: 0xba,
                        buffer:
                            '10 02 6a 00 00 00 20 20 30 30 33 32 30 30 33 33 30 30 33 34 30 30 33 35 30 30 33 36 30 30 33 37 30 30 33 38 30 30 33 39 30 30 34 30 30 30 34 31 30 30 34 32 30 30 34 33 30 30 34 34 30 30 34 35 30 30 34 36 30 30 34 37 30 30 34 38 30 30 34 39 30 30 35 30 30 30 35 31 30 30 35 32 30 30 35 33 30 30 35 34 30 30 35 35 30 30 35 36 30 30 35 37 30 30 35 38 30 30 35 39 30 30 36 30 30 30 36 31 30 30 36 32 30 30 36 33 86 ba 10 03'
                    }
                ]);
            });
        });

        describe('if [DLE] = [0x10] is found it is replaced with [DLE][DLE] to prevent the DATA, BTC or CHK being interpreted as a command', () => {
            // When a DLE is found it is replaced with DLE DLE to prevent the DATA, BTC or CHK being interpreted as a command
            let commandIdentifier: CommandIdentifier;

            beforeAll(() => {
                commandIdentifier = CommandIdentifiers.TX.GENERAL.SOURCE_ASSOCIATION_NAMES_RESPONSE_MESSAGE;
            });

            it('Should create & pack the general command (...)', () => {
                // Arrange
                const params: SourceAssociationNamesResponseCommandParams = {
                    matrixId: 0,
                    levelId: 0,
                    firstSourceId: 16,
                    numberOfSourceAssociationNamesTofollow: 16,
                    sourceAssociationNameItems: Fixture.buildCharItems(64, 8)
                };
                const options: SourceAssociationNamesResponseCommandOptions = {
                    lengthOfNames: NameLength.EIGHT_CHAR_NAMES
                };
                // Act
                const metaCommand = new SourceAssociationNamesResponseCommand(params, options);
                metaCommand.buildCommand();

                // Assert
                Fixture.assertMetaCommand(metaCommand, commandIdentifier, [
                    {
                        data:
                            '6a 00 01 00 10 10 30 30 30 30 30 30 30 30 30 30 30 30 30 30 30 31 30 30 30 30 30 30 30 32 30 30 30 30 30 30 30 33 30 30 30 30 30 30 30 34 30 30 30 30 30 30 30 35 30 30 30 30 30 30 30 36 30 30 30 30 30 30 30 37 30 30 30 30 30 30 30 38 30 30 30 30 30 30 30 39 30 30 30 30 30 30 31 30 30 30 30 30 30 30 31 31 30 30 30 30 30 30 31 32 30 30 30 30 30 30 31 33 30 30 30 30 30 30 31 34 30 30 30 30 30 30 31 35',
                        bytesCount: 0x86,
                        checksum: 0xad,
                        buffer:
                            '10 02 6a 00 01 00 10 10 10 10 30 30 30 30 30 30 30 30 30 30 30 30 30 30 30 31 30 30 30 30 30 30 30 32 30 30 30 30 30 30 30 33 30 30 30 30 30 30 30 34 30 30 30 30 30 30 30 35 30 30 30 30 30 30 30 36 30 30 30 30 30 30 30 37 30 30 30 30 30 30 30 38 30 30 30 30 30 30 30 39 30 30 30 30 30 30 31 30 30 30 30 30 30 30 31 31 30 30 30 30 30 30 31 32 30 30 30 30 30 30 31 33 30 30 30 30 30 30 31 34 30 30 30 30 30 30 31 35 86 ad 10 03'
                    },
                    {
                        data:
                            '6a 00 01 00 20 10 30 30 30 30 30 30 31 36 30 30 30 30 30 30 31 37 30 30 30 30 30 30 31 38 30 30 30 30 30 30 31 39 30 30 30 30 30 30 32 30 30 30 30 30 30 30 32 31 30 30 30 30 30 30 32 32 30 30 30 30 30 30 32 33 30 30 30 30 30 30 32 34 30 30 30 30 30 30 32 35 30 30 30 30 30 30 32 36 30 30 30 30 30 30 32 37 30 30 30 30 30 30 32 38 30 30 30 30 30 30 32 39 30 30 30 30 30 30 33 30 30 30 30 30 30 30 33 31',
                        bytesCount: 0x86,
                        checksum: 0x75,
                        buffer:
                            '10 02 6a 00 01 00 20 10 10 30 30 30 30 30 30 31 36 30 30 30 30 30 30 31 37 30 30 30 30 30 30 31 38 30 30 30 30 30 30 31 39 30 30 30 30 30 30 32 30 30 30 30 30 30 30 32 31 30 30 30 30 30 30 32 32 30 30 30 30 30 30 32 33 30 30 30 30 30 30 32 34 30 30 30 30 30 30 32 35 30 30 30 30 30 30 32 36 30 30 30 30 30 30 32 37 30 30 30 30 30 30 32 38 30 30 30 30 30 30 32 39 30 30 30 30 30 30 33 30 30 30 30 30 30 30 33 31 86 75 10 03'
                    },
                    {
                        data:
                            '6a 00 01 00 40 10 30 30 30 30 30 30 33 32 30 30 30 30 30 30 33 33 30 30 30 30 30 30 33 34 30 30 30 30 30 30 33 35 30 30 30 30 30 30 33 36 30 30 30 30 30 30 33 37 30 30 30 30 30 30 33 38 30 30 30 30 30 30 33 39 30 30 30 30 30 30 34 30 30 30 30 30 30 30 34 31 30 30 30 30 30 30 34 32 30 30 30 30 30 30 34 33 30 30 30 30 30 30 34 34 30 30 30 30 30 30 34 35 30 30 30 30 30 30 34 36 30 30 30 30 30 30 34 37',
                        bytesCount: 0x86,
                        checksum: 0x3f,
                        buffer:
                            '10 02 6a 00 01 00 40 10 10 30 30 30 30 30 30 33 32 30 30 30 30 30 30 33 33 30 30 30 30 30 30 33 34 30 30 30 30 30 30 33 35 30 30 30 30 30 30 33 36 30 30 30 30 30 30 33 37 30 30 30 30 30 30 33 38 30 30 30 30 30 30 33 39 30 30 30 30 30 30 34 30 30 30 30 30 30 30 34 31 30 30 30 30 30 30 34 32 30 30 30 30 30 30 34 33 30 30 30 30 30 30 34 34 30 30 30 30 30 30 34 35 30 30 30 30 30 30 34 36 30 30 30 30 30 30 34 37 86 3f 10 03'
                    },
                    {
                        data:
                            '6a 00 01 00 70 10 30 30 30 30 30 30 34 38 30 30 30 30 30 30 34 39 30 30 30 30 30 30 35 30 30 30 30 30 30 30 35 31 30 30 30 30 30 30 35 32 30 30 30 30 30 30 35 33 30 30 30 30 30 30 35 34 30 30 30 30 30 30 35 35 30 30 30 30 30 30 35 36 30 30 30 30 30 30 35 37 30 30 30 30 30 30 35 38 30 30 30 30 30 30 35 39 30 30 30 30 30 30 36 30 30 30 30 30 30 30 36 31 30 30 30 30 30 30 36 32 30 30 30 30 30 30 36 33',
                        bytesCount: 0x86,
                        checksum: 0xf9,
                        buffer:
                            '10 02 6a 00 01 00 70 10 10 30 30 30 30 30 30 34 38 30 30 30 30 30 30 34 39 30 30 30 30 30 30 35 30 30 30 30 30 30 30 35 31 30 30 30 30 30 30 35 32 30 30 30 30 30 30 35 33 30 30 30 30 30 30 35 34 30 30 30 30 30 30 35 35 30 30 30 30 30 30 35 36 30 30 30 30 30 30 35 37 30 30 30 30 30 30 35 38 30 30 30 30 30 30 35 39 30 30 30 30 30 30 36 30 30 30 30 30 30 30 36 31 30 30 30 30 30 30 36 32 30 30 30 30 30 30 36 33 86 f9 10 03'
                    }
                ]);
            });
        });
    });

    describe('to Log Description of the general command', () => {
        it('Should log General command description', () => {
            // Arrange
            const params: SourceAssociationNamesResponseCommandParams = {
                matrixId: 0,
                levelId: 0,
                firstSourceId: 0,
                numberOfSourceAssociationNamesTofollow: 32,
                sourceAssociationNameItems: Fixture.buildCharItems(64, 8)
            };
            const options: SourceAssociationNamesResponseCommandOptions = {
                lengthOfNames: NameLength.EIGHT_CHAR_NAMES
            };
            // Act
            const metaCommand = new SourceAssociationNamesResponseCommand(params, options);
            const description = metaCommand.toLogDescription();
            console.log(description);

            // Assert
            expect(description.toLowerCase().startsWith('general')).toBe(true);
        });
    });
});
